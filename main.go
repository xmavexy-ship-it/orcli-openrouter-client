package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// ─── Constants ───────────────────────────────────────────────────────────────

const (
	openRouterURL = "https://openrouter.ai/api/v1/chat/completions"
	configFile    = ".orcli.json"
	historyFile   = ".orcli_history.json"
)

// ─── Colors ──────────────────────────────────────────────────────────────────

var (
	bold    = color.New(color.Bold)
	cyan    = color.New(color.FgCyan, color.Bold)
	green   = color.New(color.FgGreen, color.Bold)
	yellow  = color.New(color.FgYellow, color.Bold)
	red     = color.New(color.FgRed, color.Bold)
	dim     = color.New(color.Faint)
	magenta = color.New(color.FgMagenta, color.Bold)
	blue    = color.New(color.FgBlue, color.Bold)
	white   = color.New(color.FgWhite)
)

// ─── Config ──────────────────────────────────────────────────────────────────

type Config struct {
	APIKey       string `json:"api_key"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt"`
	MaxTokens    int    `json:"max_tokens"`
	AutoApprove  bool   `json:"auto_approve"`
}

func configPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, configFile)
}

func historyPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, historyFile)
}

func loadConfig() Config {
	c := Config{
		Model:       "google/gemini-2.5-pro",
		MaxTokens:   8192,
		AutoApprove: false,
	}
	data, err := os.ReadFile(configPath())
	if err == nil {
		json.Unmarshal(data, &c)
	}
	if v := os.Getenv("OPENROUTER_API_KEY"); v != "" {
		c.APIKey = v
	}
	return c
}

func saveConfig(c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath(), data, 0600)
}

// ─── Messages & API types ────────────────────────────────────────────────────

type Message struct {
	Role       string      `json:"role"`
	Content    interface{} `json:"content"` // string or []ContentBlock
	ToolCallID string      `json:"tool_call_id,omitempty"`
}

type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type AssistantMessage struct {
	Role      string     `json:"role"`
	Content   string     `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// ─── Tool definitions ────────────────────────────────────────────────────────

type ToolParam struct {
	Type        string            `json:"type"`
	Description string            `json:"description,omitempty"`
	Enum        []string          `json:"enum,omitempty"`
	Properties  map[string]ToolParam `json:"properties,omitempty"`
	Required    []string          `json:"required,omitempty"`
	Items       *ToolParam        `json:"items,omitempty"`
}

type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string    `json:"name"`
		Description string    `json:"description"`
		Parameters  ToolParam `json:"parameters"`
	} `json:"function"`
}

func getTools() []Tool {
	return []Tool{
		{
			Type: "function",
			Function: struct {
				Name        string    `json:"name"`
				Description string    `json:"description"`
				Parameters  ToolParam `json:"parameters"`
			}{
				Name:        "read_file",
				Description: "Read the contents of a file at the given path.",
				Parameters: ToolParam{
					Type:     "object",
					Required: []string{"path"},
					Properties: map[string]ToolParam{
						"path": {Type: "string", Description: "Absolute or relative path to the file"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string    `json:"name"`
				Description string    `json:"description"`
				Parameters  ToolParam `json:"parameters"`
			}{
				Name:        "write_file",
				Description: "Write content to a file, creating it or overwriting if it exists.",
				Parameters: ToolParam{
					Type:     "object",
					Required: []string{"path", "content"},
					Properties: map[string]ToolParam{
						"path":    {Type: "string", Description: "Path to the file"},
						"content": {Type: "string", Description: "Content to write"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string    `json:"name"`
				Description string    `json:"description"`
				Parameters  ToolParam `json:"parameters"`
			}{
				Name:        "append_file",
				Description: "Append content to an existing file.",
				Parameters: ToolParam{
					Type:     "object",
					Required: []string{"path", "content"},
					Properties: map[string]ToolParam{
						"path":    {Type: "string", Description: "Path to the file"},
						"content": {Type: "string", Description: "Content to append"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string    `json:"name"`
				Description string    `json:"description"`
				Parameters  ToolParam `json:"parameters"`
			}{
				Name:        "list_dir",
				Description: "List files and directories at the given path.",
				Parameters: ToolParam{
					Type:     "object",
					Required: []string{"path"},
					Properties: map[string]ToolParam{
						"path": {Type: "string", Description: "Directory path to list"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string    `json:"name"`
				Description string    `json:"description"`
				Parameters  ToolParam `json:"parameters"`
			}{
				Name:        "run_command",
				Description: "Execute a shell command and return stdout+stderr. Use for compiling, running scripts, git, etc.",
				Parameters: ToolParam{
					Type:     "object",
					Required: []string{"command"},
					Properties: map[string]ToolParam{
						"command": {Type: "string", Description: "Shell command to run"},
						"cwd":     {Type: "string", Description: "Working directory (optional)"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string    `json:"name"`
				Description string    `json:"description"`
				Parameters  ToolParam `json:"parameters"`
			}{
				Name:        "delete_file",
				Description: "Delete a file or empty directory.",
				Parameters: ToolParam{
					Type:     "object",
					Required: []string{"path"},
					Properties: map[string]ToolParam{
						"path": {Type: "string", Description: "Path to delete"},
					},
				},
			},
		},
		{
			Type: "function",
			Function: struct {
				Name        string    `json:"name"`
				Description string    `json:"description"`
				Parameters  ToolParam `json:"parameters"`
			}{
				Name:        "search_files",
				Description: "Search for text pattern in files recursively.",
				Parameters: ToolParam{
					Type:     "object",
					Required: []string{"pattern", "path"},
					Properties: map[string]ToolParam{
						"pattern": {Type: "string", Description: "Text or regex to search for"},
						"path":    {Type: "string", Description: "Directory to search in"},
					},
				},
			},
		},
	}
}

// ─── Tool execution ───────────────────────────────────────────────────────────

// Dangerous commands that always require confirmation
var dangerousPatterns = []string{
	"rm -rf", "rm -r", "mkfs", "dd if=", ":(){:|:&};:", "chmod -R 777",
	"curl | sh", "wget | sh", "curl | bash", "wget | bash",
	"> /dev/", "format", "fdisk", "parted",
}

func isDangerous(cmd string) bool {
	lower := strings.ToLower(cmd)
	for _, p := range dangerousPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

func askConfirm(prompt string) bool {
	yellow.Printf("  ⚠  %s [y/N] ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.ToLower(strings.TrimSpace(line)) == "y"
}

type ToolArgs map[string]interface{}

func executeTool(name string, argsJSON string, autoApprove bool) string {
	var args ToolArgs
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fmt.Sprintf("error: invalid args: %v", err)
	}

	getString := func(key string) string {
		if v, ok := args[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}

	switch name {
	case "read_file":
		path := getString("path")
		blue.Printf("  📖 read_file: ")
		white.Println(path)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return string(data)

	case "write_file":
		path := getString("path")
		content := getString("content")
		blue.Printf("  ✏️  write_file: ")
		white.Println(path)
		if !autoApprove {
			// Show preview
			lines := strings.Split(content, "\n")
			preview := lines
			if len(preview) > 5 {
				preview = lines[:5]
			}
			for _, l := range preview {
				dim.Printf("     %s\n", l)
			}
			if len(lines) > 5 {
				dim.Printf("     ... (%d more lines)\n", len(lines)-5)
			}
			if !askConfirm(fmt.Sprintf("Write %d bytes to %s?", len(content), path)) {
				return "cancelled by user"
			}
		}
		// Create dirs if needed
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		green.Printf("  ✓ Written %d bytes\n", len(content))
		return fmt.Sprintf("ok: wrote %d bytes to %s", len(content), path)

	case "append_file":
		path := getString("path")
		content := getString("content")
		blue.Printf("  ✏️  append_file: ")
		white.Println(path)
		if !autoApprove {
			if !askConfirm(fmt.Sprintf("Append to %s?", path)) {
				return "cancelled by user"
			}
		}
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		defer f.Close()
		f.WriteString(content)
		return fmt.Sprintf("ok: appended to %s", path)

	case "list_dir":
		path := getString("path")
		blue.Printf("  📁 list_dir: ")
		white.Println(path)
		entries, err := os.ReadDir(path)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		var sb strings.Builder
		for _, e := range entries {
			info, _ := e.Info()
			if e.IsDir() {
				sb.WriteString(fmt.Sprintf("[DIR]  %s\n", e.Name()))
			} else {
				sb.WriteString(fmt.Sprintf("[FILE] %s  (%d bytes)\n", e.Name(), info.Size()))
			}
		}
		return sb.String()

	case "run_command":
		command := getString("command")
		cwd := getString("cwd")
		blue.Printf("  ⚡ run_command: ")
		yellow.Println(command)

		dangerous := isDangerous(command)
		if dangerous || !autoApprove {
			label := "Run this command?"
			if dangerous {
				label = "⚠  DANGEROUS command — run anyway?"
			}
			if !askConfirm(label) {
				return "cancelled by user"
			}
		}

		cmd := exec.Command("sh", "-c", command)
		if cwd != "" {
			cmd.Dir = cwd
		}
		out, err := cmd.CombinedOutput()
		result := string(out)
		if err != nil {
			result += fmt.Sprintf("\n[exit error: %v]", err)
		}
		if result == "" {
			result = "(no output)"
		}
		// Print output
		for _, line := range strings.Split(strings.TrimRight(result, "\n"), "\n") {
			dim.Printf("     %s\n", line)
		}
		return result

	case "delete_file":
		path := getString("path")
		blue.Printf("  🗑  delete_file: ")
		red.Println(path)
		if !autoApprove {
			if !askConfirm(fmt.Sprintf("Delete %s?", path)) {
				return "cancelled by user"
			}
		}
		if err := os.Remove(path); err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return fmt.Sprintf("ok: deleted %s", path)

	case "search_files":
		pattern := getString("pattern")
		path := getString("path")
		blue.Printf("  🔍 search_files: ")
		white.Printf("%q in %s\n", pattern, path)
		cmd := exec.Command("grep", "-rn", "--include=*", pattern, path)
		out, _ := cmd.CombinedOutput()
		if len(out) == 0 {
			return "no matches found"
		}
		lines := strings.Split(string(out), "\n")
		if len(lines) > 50 {
			lines = lines[:50]
			lines = append(lines, fmt.Sprintf("... (truncated, showing 50 of many results)"))
		}
		return strings.Join(lines, "\n")

	default:
		return fmt.Sprintf("error: unknown tool %q", name)
	}
}

// ─── API call ─────────────────────────────────────────────────────────────────

type ChatRequest struct {
	Model     string      `json:"model"`
	Messages  interface{} `json:"messages"`
	MaxTokens int         `json:"max_tokens,omitempty"`
	Stream    bool        `json:"stream"`
	Tools     []Tool      `json:"tools,omitempty"`
}

type NonStreamResponse struct {
	Choices []struct {
		Message AssistantMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

type StreamChoice struct {
	Delta struct {
		Content   string     `json:"content"`
		ToolCalls []ToolCall `json:"tool_calls"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

type StreamChunk struct {
	Choices []StreamChoice `json:"choices"`
}

// callAPI sends messages and returns the assistant's response.
// If the model wants to call tools, it returns them without streaming.
// If it's a regular text response, it streams to stdout.
func callAPI(cfg Config, messages []interface{}) (AssistantMessage, error) {
	tools := getTools()

	req := ChatRequest{
		Model:     cfg.Model,
		Messages:  messages,
		MaxTokens: cfg.MaxTokens,
		Tools:     tools,
		Stream:    true,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return AssistantMessage{}, err
	}

	httpReq, err := http.NewRequest("POST", openRouterURL, bytes.NewReader(body))
	if err != nil {
		return AssistantMessage{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://github.com/orcli")
	httpReq.Header.Set("X-Title", "orcli")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return AssistantMessage{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return AssistantMessage{}, fmt.Errorf("API error %d: %s", resp.StatusCode, string(b))
	}

	// Stream and collect
	var fullText strings.Builder
	// tool_calls accumulate across chunks
	toolCallsMap := map[int]*ToolCall{}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	printedHeader := false

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta

		// Accumulate tool calls
		for _, tc := range delta.ToolCalls {
			if _, ok := toolCallsMap[tc.Index]; !ok {
				// Use Index field if available, else use loop index
				idx := 0
				if tc.Index != 0 {
					idx = tc.Index
				} else {
					idx = len(toolCallsMap)
				}
				toolCallsMap[idx] = &ToolCall{
					ID:   tc.ID,
					Type: "function",
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name: tc.Function.Name,
					},
				}
			}
			toolCallsMap[len(toolCallsMap)-1].Function.Arguments += tc.Function.Arguments
		}

		// Stream text
		if delta.Content != "" {
			if !printedHeader {
				fmt.Println()
				magenta.Print("  ◆ ")
				bold.Println("Assistant")
				fmt.Print("  ")
				printedHeader = true
			}
			parts := strings.Split(delta.Content, "\n")
			for i, part := range parts {
				fmt.Print(part)
				if i < len(parts)-1 {
					fmt.Print("\n  ")
				}
			}
			fullText.WriteString(delta.Content)
		}
	}

	if printedHeader {
		fmt.Println("\n")
	}

	// Build result
	result := AssistantMessage{
		Role:    "assistant",
		Content: fullText.String(),
	}

	// Collect tool calls
	for i := 0; i < len(toolCallsMap); i++ {
		if tc, ok := toolCallsMap[i]; ok {
			result.ToolCalls = append(result.ToolCalls, *tc)
		}
	}

	return result, scanner.Err()
}

// ─── Agent loop ───────────────────────────────────────────────────────────────

func runAgentLoop(cfg Config, messages []interface{}) []interface{} {
	for {
		response, err := callAPI(cfg, messages)
		if err != nil {
			red.Printf("  ✗ API error: %v\n\n", err)
			return messages
		}

		// Add assistant message to history
		messages = append(messages, response)

		// No tool calls → done
		if len(response.ToolCalls) == 0 {
			return messages
		}

		// Execute each tool call
		fmt.Println()
		cyan.Println("  ┄ tool calls ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄")

		for _, tc := range response.ToolCalls {
			fmt.Println()
			result := executeTool(tc.Function.Name, tc.Function.Arguments, cfg.AutoApprove)

			// Add tool result message
			messages = append(messages, map[string]interface{}{
				"role":         "tool",
				"tool_call_id": tc.ID,
				"content":      result,
			})
		}

		cyan.Println("\n  ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄")
		fmt.Println()

		// Continue loop — model will respond to tool results
	}
}

// ─── Session ─────────────────────────────────────────────────────────────────

type Session struct {
	Messages  []interface{} `json:"messages"`
	CreatedAt time.Time     `json:"created_at"`
}

func saveHistory(s Session) {
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(historyPath(), data, 0600)
}

func loadHistory() Session {
	var s Session
	data, err := os.ReadFile(historyPath())
	if err == nil {
		json.Unmarshal(data, &s)
	}
	return s
}

// ─── UI ───────────────────────────────────────────────────────────────────────

func printBanner() {
	fmt.Println()
	cyan.Println("  ╔════════════════════════════════════╗")
	cyan.Print("  ║  ")
	bold.Print("  ⬡  orcli  —  OpenRouter Agent CLI  ")
	cyan.Println("║")
	cyan.Println("  ╚════════════════════════════════════╝")
	fmt.Println()
}

func printHelp() {
	yellow.Println("  Commands:")
	fmt.Println()
	dim.Println("  /help              show this help")
	dim.Println("  /model <name>      switch model")
	dim.Println("  /auto              toggle auto-approve tools (off by default)")
	dim.Println("  /system <msg>      set system prompt")
	dim.Println("  /clear             clear conversation")
	dim.Println("  /history           show conversation summary")
	dim.Println("  /save              save session to disk")
	dim.Println("  /load              load last session")
	dim.Println("  /tools             list available tools")
	dim.Println("  /config            show current config")
	dim.Println("  /cwd               show current working directory")
	dim.Println("  /cd <path>         change working directory")
	dim.Println("  /quit              exit")
	fmt.Println()
	yellow.Println("  Tips:")
	dim.Println("  • The AI can read/write files, run commands, search code")
	dim.Println("  • It will ask for confirmation before each action")
	dim.Println("  • Use /auto to skip confirmations (be careful!)")
	dim.Println("  • Include file paths in your message for context")
	fmt.Println()
}

func printTools() {
	yellow.Println("  Available tools:")
	fmt.Println()
	tools := [][]string{
		{"read_file", "Read any file"},
		{"write_file", "Create or overwrite a file"},
		{"append_file", "Append to a file"},
		{"list_dir", "List directory contents"},
		{"run_command", "Execute shell commands"},
		{"delete_file", "Delete a file"},
		{"search_files", "grep-search in files"},
	}
	for _, t := range tools {
		green.Printf("  %-18s", t[0])
		dim.Println(t[1])
	}
	fmt.Println()
}

// ─── Main chat ────────────────────────────────────────────────────────────────

var cfg Config

func runChat(oneShot string) {
	cfg = loadConfig()
	if cfg.APIKey == "" {
		red.Println("\n  ✗ API key not set!")
		fmt.Println("  Run:    orcli config --key YOUR_KEY")
		fmt.Println("  Or set: export OPENROUTER_API_KEY=YOUR_KEY\n")
		os.Exit(1)
	}

	systemPrompt := cfg.SystemPrompt
	if systemPrompt == "" {
		cwd, _ := os.Getwd()
		systemPrompt = fmt.Sprintf(`You are orcli, a powerful terminal AI agent running on the user's machine.
You have access to tools: read_file, write_file, append_file, list_dir, run_command, delete_file, search_files.
Current working directory: %s
OS: Linux (Arch Linux)

Be concise. When asked to do something with files or code — do it directly using tools.
Think step by step, use multiple tools when needed, explain what you're doing.`, cwd)
	}

	messages := []interface{}{
		map[string]interface{}{"role": "system", "content": systemPrompt},
	}

	// One-shot mode
	if oneShot != "" {
		messages = append(messages, map[string]interface{}{"role": "user", "content": oneShot})
		runAgentLoop(cfg, messages)
		return
	}

	// Interactive
	printBanner()
	green.Printf("  Model:       ")
	fmt.Println(cfg.Model)
	green.Printf("  Auto-approve: ")
	if cfg.AutoApprove {
		red.Println("ON (danger!)")
	} else {
		dim.Println("off (will ask before each action)")
	}
	fmt.Println()
	dim.Println("  Type /help for commands. Ask me anything — I can read/write files and run commands.")
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)

	for {
		// Prompt
		cwd, _ := os.Getwd()
		home, _ := os.UserHomeDir()
		cwdDisplay := strings.Replace(cwd, home, "~", 1)

		cyan.Printf("  [%s] ", cwdDisplay)
		bold.Print("▸ ")

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println()
				dim.Println("  Bye!")
				return
			}
			return
		}

		input := strings.TrimSpace(line)
		if input == "" {
			continue
		}

		switch {
		case input == "/quit" || input == "/exit" || input == "/q":
			dim.Println("  Bye!")
			return

		case input == "/help":
			printHelp()

		case input == "/tools":
			printTools()

		case input == "/auto":
			cfg.AutoApprove = !cfg.AutoApprove
			if cfg.AutoApprove {
				red.Println("  ⚡ Auto-approve ON — commands will run without confirmation!")
			} else {
				green.Println("  ✓ Auto-approve OFF — will ask before each action")
			}
			fmt.Println()

		case input == "/clear":
			cwd, _ := os.Getwd()
			messages = []interface{}{
				map[string]interface{}{"role": "system", "content": fmt.Sprintf(
					"You are orcli, a terminal AI agent. CWD: %s", cwd,
				)},
			}
			green.Println("  ✓ Conversation cleared\n")

		case input == "/history":
			for i, m := range messages {
				if mm, ok := m.(map[string]interface{}); ok {
					role := fmt.Sprintf("%v", mm["role"])
					content := fmt.Sprintf("%v", mm["content"])
					if len(content) > 60 {
						content = content[:60] + "…"
					}
					switch role {
					case "system":
						yellow.Printf("  [%d] system:    ", i)
					case "user":
						cyan.Printf("  [%d] user:      ", i)
					case "assistant":
						magenta.Printf("  [%d] assistant: ", i)
					case "tool":
						blue.Printf("  [%d] tool:      ", i)
					}
					dim.Println(content)
				}
			}
			fmt.Println()

		case input == "/save":
			saveHistory(Session{Messages: messages, CreatedAt: time.Now()})
			green.Println("  ✓ Session saved\n")

		case input == "/load":
			s := loadHistory()
			messages = s.Messages
			green.Printf("  ✓ Loaded session (%d messages)\n\n", len(messages))

		case input == "/config":
			yellow.Println("  Config:")
			fmt.Printf("  model:        %s\n", cfg.Model)
			fmt.Printf("  max_tokens:   %d\n", cfg.MaxTokens)
			fmt.Printf("  auto_approve: %v\n", cfg.AutoApprove)
			keyPreview := "not set"
			if cfg.APIKey != "" {
				keyPreview = cfg.APIKey[:min(8, len(cfg.APIKey))] + "..."
			}
			fmt.Printf("  api_key:      %s\n\n", keyPreview)

		case input == "/cwd":
			cwd, _ := os.Getwd()
			fmt.Printf("  %s\n\n", cwd)

		case strings.HasPrefix(input, "/cd "):
			path := strings.TrimPrefix(input, "/cd ")
			if err := os.Chdir(path); err != nil {
				red.Printf("  ✗ %v\n\n", err)
			} else {
				cwd, _ := os.Getwd()
				green.Printf("  ✓ Now in: %s\n\n", cwd)
			}

		case strings.HasPrefix(input, "/model "):
			cfg.Model = strings.TrimPrefix(input, "/model ")
			green.Printf("  ✓ Model: %s\n\n", cfg.Model)

		case strings.HasPrefix(input, "/system "):
			sys := strings.TrimPrefix(input, "/system ")
			messages[0] = map[string]interface{}{"role": "system", "content": sys}
			green.Println("  ✓ System prompt updated\n")

		default:
			messages = append(messages, map[string]interface{}{"role": "user", "content": input})
			messages = runAgentLoop(cfg, messages)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ─── ToolCall index field ─────────────────────────────────────────────────────
// We need Index on ToolCall for streaming accumulation

func init() {
	// patch: ToolCall needs Index for streaming
}

// Override ToolCall to add Index (for streaming delta accumulation)
type ToolCallDelta struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	Type  string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ─── Cobra CLI ────────────────────────────────────────────────────────────────

func main() {
	var rootCmd = &cobra.Command{
		Use:   "orcli [message]",
		Short: "OpenRouter Agent CLI — AI with file & shell access",
		Args:  cobra.ArbitraryArgs,
		Run: func(cmd *cobra.Command, args []string) {
			runChat(strings.Join(args, " "))
		},
	}

	var configCmd = &cobra.Command{
		Use:   "config",
		Short: "Set API key, model, and other options",
		Run: func(cmd *cobra.Command, args []string) {
			c := loadConfig()
			key, _ := cmd.Flags().GetString("key")
			model, _ := cmd.Flags().GetString("model")
			system, _ := cmd.Flags().GetString("system")
			tokens, _ := cmd.Flags().GetInt("max-tokens")
			auto, _ := cmd.Flags().GetBool("auto")

			if key != "" { c.APIKey = key }
			if model != "" { c.Model = model }
			if system != "" { c.SystemPrompt = system }
			if tokens > 0 { c.MaxTokens = tokens }
			if auto { c.AutoApprove = true }

			if err := saveConfig(c); err != nil {
				red.Printf("  ✗ %v\n", err)
				os.Exit(1)
			}
			green.Println("  ✓ Config saved (~/.orcli.json)")
			fmt.Printf("  model:        %s\n", c.Model)
			fmt.Printf("  max_tokens:   %d\n", c.MaxTokens)
			fmt.Printf("  auto_approve: %v\n\n", c.AutoApprove)
		},
	}
	configCmd.Flags().String("key", "", "OpenRouter API key")
	configCmd.Flags().String("model", "", "Default model")
	configCmd.Flags().String("system", "", "System prompt")
	configCmd.Flags().Int("max-tokens", 0, "Max tokens")
	configCmd.Flags().Bool("auto", false, "Enable auto-approve")

	var modelsCmd = &cobra.Command{
		Use:   "models",
		Short: "Show popular models",
		Run: func(cmd *cobra.Command, args []string) {
			yellow.Println("\n  Popular models on OpenRouter:\n")
			models := [][]string{
				{"google/gemini-2.5-pro",          "Best overall, huge context"},
				{"anthropic/claude-sonnet-4-5",    "Fast and very capable"},
				{"anthropic/claude-opus-4-5",      "Most capable Claude"},
				{"openai/gpt-4o",                  "OpenAI flagship"},
				{"openai/o3-mini",                 "Reasoning model"},
				{"deepseek/deepseek-r1",           "Reasoning, very cheap"},
				{"meta-llama/llama-4-maverick",    "Open source, fast"},
				{"mistralai/mistral-large",        "EU-based, strong"},
				{"qwen/qwen-2.5-coder-32b-instruct","Best for code"},
			}
			for _, m := range models {
				cyan.Printf("  %-42s", m[0])
				dim.Println(m[1])
			}
			fmt.Println()
			dim.Println("  Full list: https://openrouter.ai/models\n")
		},
	}

	rootCmd.AddCommand(configCmd, modelsCmd)
	rootCmd.Execute()
}
