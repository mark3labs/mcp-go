//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"kit/ext"
)

const (
	diagnosticsTimeout = 20 * time.Second
	maxOutputBytes     = 12_000
)

type toolPathInput struct {
	Path string `json:"path"`
}

type lintResult struct {
	Output string
	Err    error
}

// Package-level state: set of .go files edited during the current agent turn.
var editedFiles map[string]bool

func Init(api ext.API) {
	api.OnSessionStart(func(_ ext.SessionStartEvent, ctx ext.Context) {
		ctx.Print("go-edit-lint extension loaded - will run gopls and golangci-lint after agent turns that edit Go files")
	})

	// Track edited .go files — don't lint yet.
	api.OnToolResult(func(e ext.ToolResultEvent, ctx ext.Context) *ext.ToolResultResult {
		if e.IsError || !isEditOrWrite(e.ToolName) {
			return nil
		}

		absPath, ok := resolveGoFilePath(e.Input, ctx.CWD)
		if !ok {
			return nil
		}

		if editedFiles == nil {
			editedFiles = make(map[string]bool)
		}
		editedFiles[absPath] = true
		return nil
	})

	// After the agent turn ends, lint all collected files.
	api.OnAgentEnd(func(e ext.AgentEndEvent, ctx ext.Context) {
		if len(editedFiles) == 0 {
			return
		}

		// Snapshot and reset immediately so the next turn starts clean.
		files := editedFiles
		editedFiles = nil

		// Skip lint on errored turns.
		if e.StopReason == "error" {
			return
		}

		// Collect unique directories and file list for gopls.
		var allGoplsOutput []string
		for absPath := range files {
			res := runGopls(ctx.CWD, absPath)
			formatted := formatToolResult(res, "")
			if formatted != "" {
				allGoplsOutput = append(allGoplsOutput, fmt.Sprintf("# %s\n%s", filepath.Base(absPath), formatted))
			}
		}

		lintRes := runGolangCILint(ctx.CWD, "./...")

		goplsSection := "No diagnostics."
		if len(allGoplsOutput) > 0 {
			goplsSection = strings.Join(allGoplsOutput, "\n\n")
		}
		lintSection := formatToolResult(lintRes, "No lint issues.")

		// Build file list for the report header.
		var fileNames []string
		for absPath := range files {
			fileNames = append(fileNames, filepath.Base(absPath))
		}

		report := fmt.Sprintf(
			"<go_diagnostics files=%q>\n[gopls]\n%s\n\n[golangci-lint]\n%s\n</go_diagnostics>",
			strings.Join(fileNames, ", "),
			goplsSection,
			lintSection,
		)

		goplsIssues, lintIssues := countIssues(report)
		hasIssues := goplsIssues > 0 || lintIssues > 0

		if hasIssues {
			// Show TUI block so the user sees it too.
			var msgLines []string
			msgLines = append(msgLines, fmt.Sprintf("Files: %s", strings.Join(fileNames, ", ")))
			if goplsIssues > 0 {
				msgLines = append(msgLines, fmt.Sprintf("gopls: %d issue(s)", goplsIssues))
			}
			if lintIssues > 0 {
				msgLines = append(msgLines, fmt.Sprintf("golangci-lint: %d issue(s)", lintIssues))
			}

			borderColor := "#f9e2af" // yellow
			if goplsIssues > 0 && lintIssues > 0 {
				borderColor = "#f38ba8" // red
			}

			ctx.PrintBlock(ext.PrintBlockOpts{
				Text:        strings.Join(msgLines, "\n"),
				BorderColor: borderColor,
				Subtitle:    "go-edit-lint",
			})

			// Inject a follow-up message so the agent fixes the issues.
			ctx.SendMessage(report + "\n\n⚠️ DIAGNOSTICS FOUND: Please review and fix the issues above.")
		} else {
			ctx.PrintBlock(ext.PrintBlockOpts{
				Text:        fmt.Sprintf("Files: %s\n✓ All clean", strings.Join(fileNames, ", ")),
				BorderColor: "#a6e3a1",
				Subtitle:    "go-edit-lint",
			})
		}
	})
}

func isEditOrWrite(toolName string) bool {
	return strings.EqualFold(toolName, "edit") || strings.EqualFold(toolName, "write")
}

func resolveGoFilePath(inputJSON, cwd string) (string, bool) {
	var args toolPathInput
	if err := json.Unmarshal([]byte(inputJSON), &args); err != nil || args.Path == "" {
		return "", false
	}

	absPath := args.Path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(cwd, absPath)
	}

	if strings.ToLower(filepath.Ext(absPath)) != ".go" {
		return "", false
	}

	return absPath, true
}

func runGopls(cwd, absPath string) lintResult {
	ctx, cancel := context.WithTimeout(context.Background(), diagnosticsTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gopls", "check", absPath)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return lintResult{Err: fmt.Errorf("timed out after %s", diagnosticsTimeout)}
	}

	if err != nil {
		return lintResult{Output: truncate(string(out), maxOutputBytes), Err: fmt.Errorf("failed to run gopls check: %w", err)}
	}

	return lintResult{Output: truncate(string(out), maxOutputBytes)}
}

func runGolangCILint(cwd, target string) lintResult {
	ctx, cancel := context.WithTimeout(context.Background(), diagnosticsTimeout)
	defer cancel()

	args := []string{
		"run",
		target,
		"--show-stats=false",
		"--output.text.path", "stdout",
		"--output.text.colors=false",
		"--output.text.print-issued-lines=false",
	}
	cmd := exec.CommandContext(ctx, "golangci-lint", args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return lintResult{Err: fmt.Errorf("timed out after %s", diagnosticsTimeout)}
	}

	trimmed := truncate(string(out), maxOutputBytes)
	if err == nil {
		return lintResult{Output: trimmed}
	}

	exitErr, ok := err.(*exec.ExitError)
	if ok && exitErr.ExitCode() == 1 {
		return lintResult{Output: trimmed}
	}

	return lintResult{Output: trimmed, Err: fmt.Errorf("failed to run golangci-lint: %w", err)}
}

func formatToolResult(res lintResult, emptyFallback string) string {
	var lines []string
	if res.Err != nil {
		lines = append(lines, "ERROR: "+res.Err.Error())
	}
	out := strings.TrimSpace(res.Output)
	if out == "" {
		if res.Err == nil {
			if emptyFallback != "" {
				lines = append(lines, emptyFallback)
			}
		}
	} else {
		lines = append(lines, out)
	}
	if len(lines) == 0 {
		return emptyFallback
	}
	return strings.Join(lines, "\n")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... output truncated ..."
}

func countIssues(report string) (goplsCount, lintCount int) {
	goplsStart := strings.Index(report, "[gopls]")
	lintStart := strings.Index(report, "[golangci-lint]")
	endTag := strings.Index(report, "</go_diagnostics>")

	if goplsStart != -1 && lintStart != -1 {
		goplsSection := report[goplsStart:lintStart]
		for _, line := range strings.Split(goplsSection, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && line != "[gopls]" && line != "No diagnostics." && !strings.HasPrefix(line, "#") {
				goplsCount++
			}
		}
	}

	if lintStart != -1 && endTag != -1 {
		lintSection := report[lintStart:endTag]
		for _, line := range strings.Split(lintSection, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && line != "[golangci-lint]" && line != "No lint issues." {
				lintCount++
			}
		}
	}

	return goplsCount, lintCount
}
