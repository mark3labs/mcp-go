package main

import (
	_ "embed"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"
)

//go:generate go run . ../..

//go:embed hooks.go.tmpl
var hooksTemplate string

//go:embed request_handler.go.tmpl
var requestHandlerTemplate string

func RenderTemplateToFile(templateContent, destPath, fileName string, data any) error {
	// Create temp file for initial output
	tempFile, err := os.CreateTemp("", "hooks-*.go")
	if err != nil {
		return err
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath) // Clean up temp file when done
	defer tempFile.Close()

	// Parse and execute template to temp file
	tmpl, err := template.New(fileName).Funcs(template.FuncMap{
		"toLower": strings.ToLower,
	}).Parse(templateContent)
	if err != nil {
		return err
	}

	if err := tmpl.Execute(tempFile, data); err != nil {
		return err
	}

	// Run goimports on the temp file
	cmd := exec.Command("go", "run", "golang.org/x/tools/cmd/goimports@latest", "-w", tempFilePath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("goimports failed: %v\n%s", err, output)
	}

	// Read the processed content
	processedContent, err := os.ReadFile(tempFilePath)
	if err != nil {
		return err
	}

	// Write the processed content to the destination
	var destWriter io.Writer
	if destPath == "-" {
		destWriter = os.Stdout
	} else {
		destFile, err := os.Create(filepath.Join(destPath, fileName))
		if err != nil {
			return err
		}
		defer destFile.Close()
		destWriter = destFile
	}

	_, err = destWriter.Write(processedContent)
	return err
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: gen <destination-directory>")
	}
	destPath := os.Args[1]

	if err := RenderTemplateToFile(hooksTemplate, destPath, "hooks.go", MCPRequestTypes); err != nil {
		log.Fatal(err)
	}

	if err := RenderTemplateToFile(requestHandlerTemplate, destPath, "request_handler.go", MCPRequestTypes); err != nil {
		log.Fatal(err)
	}
}
