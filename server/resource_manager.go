// Package server provides MCP (Model Context Protocol) server implementations.
package server

import (
	"container/list"
	"context"
	"encoding/base64"
	"fmt"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/util"
)

// resourceEntry holds both a resource and its handler
type resourceEntry struct {
	resource mcp.Resource
	handler  ResourceHandlerFunc
}

// resourceTemplateEntry holds both a template and its handler
type resourceTemplateEntry struct {
	template mcp.ResourceTemplate
	handler  ResourceTemplateHandlerFunc
}

// ResourceHandlerFunc is a function that returns resource contents.
type ResourceHandlerFunc func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)

// ResourceTemplateHandlerFunc is a function that returns a resource template.
type ResourceTemplateHandlerFunc func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error)

// ServerResource combines a Resource with its handler function.
type ServerResource struct {
	Resource mcp.Resource
	Handler  ResourceHandlerFunc
}

// resourceManager manages the lifecycle of resources and resource templates within MCPServer.
//
// It ensures that:
//  1. Resources and templates are stored and returned in insertion order.
//  2. Lookup and deletion operations are performed in O(1) time using an internal map.
//
// Internally, it maintains:
//   - A doubly linked list (`resourcesList`, `resourceTemplatesList`) to preserve insertion order.
//   - A hash map (`resourcesDict`, `resourceTemplatesDict`) for fast lookup and deletion by URI.
//
// When a resource with the same URI is added more than once, the behavior depends on
// the warnOnDuplicateResources flag:
//   - If true: a warning is logged via internal logger, and the new resource is ignored.
//   - If false: the previous entry is removed from the list and replaced by the new one.
type resourceManager struct {
	resourcesMu sync.RWMutex

	resourcesList *list.List
	resourcesDict map[string]*list.Element

	resourceTemplatesList *list.List
	resourceTemplatesDict map[string]*list.Element

	warnOnDuplicateResources bool
	logger                   util.Logger
}

func newResourceManager() *resourceManager {
	return &resourceManager{
		resourcesList:         list.New(),
		resourcesDict:         make(map[string]*list.Element),
		resourceTemplatesList: list.New(),
		resourceTemplatesDict: make(map[string]*list.Element),

		warnOnDuplicateResources: true,
		logger:                   util.DefaultLogger(),
	}
}

// addResources registers multiple resources at once.
// If a resource with the same URI already exists:
//   - If warnOnDuplicateResources is true, it logs a warning and skips the resource.
//   - Otherwise, it replaces the old resource by removing its list element and inserting the new one.
//
// Returns true if the resource list was modified.
func (rm *resourceManager) addResources(resources ...ServerResource) (listChanged bool) {
	rm.resourcesMu.Lock()
	for _, r := range resources {
		elem, exists := rm.resourcesDict[r.Resource.URI]
		if exists {
			if rm.warnOnDuplicateResources {
				rm.logger.Warnf("resource already exists, skipped: %s", r.Resource.URI)
				continue
			}
			// Remove old element from list before replacing
			rm.resourcesList.Remove(elem)
		}

		// Add new resource to list and update map
		elem = rm.resourcesList.PushBack(resourceEntry{
			resource: r.Resource,
			handler:  r.Handler,
		})
		rm.resourcesDict[r.Resource.URI] = elem
		listChanged = true
	}
	rm.resourcesMu.Unlock()

	return
}

// removeResource removes a resource by its URI.
// Returns true if a resource was actually removed.
func (rm *resourceManager) removeResource(uri string) bool {
	rm.resourcesMu.Lock()
	defer rm.resourcesMu.Unlock()

	if elem, exists := rm.resourcesDict[uri]; exists {
		rm.resourcesList.Remove(elem)
		delete(rm.resourcesDict, uri)
		return true
	}
	return false
}

// addResourceTemplate registers a new resource template and its handler,
// If a resource template with the same URI template already exists,
// it replaces the old template by removing its list element and inserting the new one.
//
// Returns true if the resource template list was modified.
func (rm *resourceManager) addResourceTemplate(
	template mcp.ResourceTemplate,
	handler ResourceTemplateHandlerFunc,
) (listChanged bool) {
	rm.resourcesMu.Lock()
	defer rm.resourcesMu.Unlock()

	elem, exists := rm.resourceTemplatesDict[template.URITemplate.Raw()]
	if exists {
		// Remove old element from list before replacing
		rm.resourceTemplatesList.Remove(elem)
	}
	// Add new template to list and update map
	elem = rm.resourceTemplatesList.PushBack(resourceTemplateEntry{
		template: template,
		handler:  handler,
	})
	rm.resourceTemplatesDict[template.URITemplate.Raw()] = elem
	listChanged = true

	return
}

// listResources returns a paginated list of resources in insertion order.
//
// The returned list starts after the resource identified by the provided cursor.
// If no cursor is provided, listing starts from the beginning.
// Pagination is limited by paginationLimit if provided.
//
// The returned cursor (if any) points to the last resource in the current page,
// allowing clients to resume from the next resource in the following request.
func (rm *resourceManager) listResources(
	cursor mcp.Cursor,
	paginationLimit *int,
) ([]mcp.Resource, mcp.Cursor, error) {
	rm.resourcesMu.RLock()
	defer rm.resourcesMu.RUnlock()

	startElem := rm.resourcesList.Front()
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(string(cursor))
		if err != nil {
			return nil, "", err
		}
		if e, ok := rm.resourcesDict[string(decoded)]; ok {
			startElem = e.Next()
		}
	}

	resourcesToReturn := []mcp.Resource{}
	lastURI := ""
	ptr := startElem
	// Determine iteration limit: if no pagination limit, walk entire list
	// Condition statement "ptr != nil" in following FOR loop ensures that resourcesToReturn only includes the resources after startElem
	limit := rm.resourcesList.Len()
	if paginationLimit != nil {
		limit = min(limit, *paginationLimit)
	}
	// Iterate up to 'limit' elements or until list ends
	for ptr != nil && len(resourcesToReturn) < limit {
		entry := ptr.Value.(resourceEntry)
		resourcesToReturn = append(resourcesToReturn, entry.resource)
		lastURI = entry.resource.URI
		ptr = ptr.Next()
	}

	var nextCursor mcp.Cursor
	// Only generate cursor if there are more elements after current page
	// The returned cursor points to the last returned resource's URI.
	// On the next request, pagination will resume from the next element (exclusive).
	if paginationLimit != nil && len(resourcesToReturn) >= *paginationLimit && ptr != nil {
		encoded := base64.StdEncoding.EncodeToString([]byte(lastURI))
		nextCursor = mcp.Cursor(encoded)
	}

	return resourcesToReturn, nextCursor, nil
}

// listResourceTemplates returns a paginated list of resource templates in insertion order.
//
// The returned list starts after the template identified by the provided cursor.
// If no cursor is provided, listing starts from the beginning.
// Pagination is limited by paginationLimit if provided.
//
// The returned cursor (if any) points to the last template in the current page,
// allowing clients to resume from the next template in the following request.
func (rm *resourceManager) listResourceTemplates(
	cursor mcp.Cursor,
	paginationLimit *int,
) ([]mcp.ResourceTemplate, mcp.Cursor, error) {
	rm.resourcesMu.RLock()
	defer rm.resourcesMu.RUnlock()

	startElem := rm.resourceTemplatesList.Front()
	if cursor != "" {
		decoded, err := base64.StdEncoding.DecodeString(string(cursor))
		if err != nil {
			return nil, "", err
		}
		if e, ok := rm.resourceTemplatesDict[string(decoded)]; ok {
			startElem = e.Next()
		}
	}

	templatesToReturn := []mcp.ResourceTemplate{}
	lastTemplate := ""
	ptr := startElem
	// Determine iteration limit: if no pagination limit, walk entire list
	// Condition statement "ptr != nil" in following FOR loop ensures that templatesToReturn only includes the templates after startElem
	limit := rm.resourceTemplatesList.Len()
	if paginationLimit != nil {
		limit = min(limit, *paginationLimit)
	}
	// Iterate up to 'limit' elements or until list ends
	for ptr != nil && len(templatesToReturn) < limit {
		entry := ptr.Value.(resourceTemplateEntry)
		templatesToReturn = append(templatesToReturn, entry.template)
		lastTemplate = entry.template.URITemplate.Raw()
		ptr = ptr.Next()
	}

	var nextCursor mcp.Cursor
	// Only generate cursor if there are more elements after current page
	// The returned cursor points to the last returned template's URI.
	// On the next request, pagination will resume from the next element (exclusive).
	if paginationLimit != nil && len(templatesToReturn) >= *paginationLimit && ptr != nil {
		encoded := base64.StdEncoding.EncodeToString([]byte(lastTemplate))
		nextCursor = mcp.Cursor(encoded)
	}
	return templatesToReturn, nextCursor, nil
}

func (rm *resourceManager) readResource(
	ctx context.Context,
	id any,
	request mcp.ReadResourceRequest,
) (*mcp.ReadResourceResult, *requestError) {
	rm.resourcesMu.RLock()
	// First try direct resource handlers
	if elem, ok := rm.resourcesDict[request.Params.URI]; ok {
		entry := elem.Value.(resourceEntry)
		handler := entry.handler
		rm.resourcesMu.RUnlock()
		contents, err := handler(ctx, request)
		if err != nil {
			return nil, &requestError{
				id:   id,
				code: mcp.INTERNAL_ERROR,
				err:  err,
			}
		}
		return &mcp.ReadResourceResult{Contents: contents}, nil
	}

	// If no direct handler found, try matching against templates
	var matchedHandler ResourceTemplateHandlerFunc
	var matched bool
	for _, elem := range rm.resourceTemplatesDict {
		entry := elem.Value.(resourceTemplateEntry)
		template := entry.template
		if matchesTemplate(request.Params.URI, template.URITemplate) {
			matchedHandler = entry.handler
			matched = true
			matchedVars := template.URITemplate.Match(request.Params.URI)
			// Convert matched variables to a map
			request.Params.Arguments = make(map[string]any, len(matchedVars))
			for name, value := range matchedVars {
				request.Params.Arguments[name] = value.V
			}
			break
		}
	}
	rm.resourcesMu.RUnlock()

	if matched {
		contents, err := matchedHandler(ctx, request)
		if err != nil {
			return nil, &requestError{
				id:   id,
				code: mcp.INTERNAL_ERROR,
				err:  err,
			}
		}
		return &mcp.ReadResourceResult{Contents: contents}, nil
	}

	return nil, &requestError{
		id:   id,
		code: mcp.RESOURCE_NOT_FOUND,
		err: fmt.Errorf(
			"handler not found for resource URI '%s': %w",
			request.Params.URI,
			ErrResourceNotFound,
		),
	}
}

// matchesTemplate checks if a URI matches a URI template pattern
func matchesTemplate(uri string, template *mcp.URITemplate) bool {
	return template.Regexp().MatchString(uri)
}
