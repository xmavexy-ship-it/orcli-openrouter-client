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
	geminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"
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
		Model:       "gemini-2.5-pro",
		MaxTokens:   8192,
		AutoApprove: false,
	}
	data, err := os.ReadFile(configPath())
	if err == nil {
		json.Unmarshal(data, &c)
	}
	if v := os.Getenv("GEMINI_API_KEY"); v != "" {
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

// ─── Gemini API types ─────────────────────────────────────────────────────────

// Входящие сообщения

type GeminiPart struct {
	Text         string        `json:"text,omitempty"`
	FunctionCall *FunctionCall `json:"functionCall,omitempty"`
	FunctionResp *FunctionResp `json:"functionResponse,omitempty"`
}

type FunctionCall struct {
	Name string                 `json:"name"`
	Args map[string]interface{} `json:"args"`
}

type FunctionResp struct {
	Name     string                 `json:"name"`
	Response map[string]interface{} `json:"response"`
}

type GeminiContent struct {
	Role  string       `json:"role"` // "user" | "model"
	Parts []GeminiPart `json:"parts"`
}

// Tool declarations

type GeminiTool struct {
	FunctionDeclarations []FunctionDecl `json:"functionDeclarations"`
}

type FunctionDecl struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Parameters  SchemaNode `json:"parameters"`
}

type SchemaNode struct {
	Type        string                `json:"type,omitempty"`
	Description string                `json:"description,omitempty"`
	Properties  map[string]SchemaNode `json:"properties,omitempty"`
	Required    []string              `json:"required,omitempty"`
	Items       *SchemaNode           `json:"items,omitempty"`
}

// Request

type GenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type SystemInstruction struct {
	Parts []GeminiPart `json:"parts"`
}

type GeminiRequest struct {
	Contents          []GeminiContent   `json:"contents"`
	Tools             []GeminiTool      `json:"tools,omitempty"`
	GenerationConfig  GenerationConfig  `json:"generationConfig,omitempty"`
	SystemInstruction SystemInstruction `json:"systemInstruction,omitempty"`
}

// Response (non-stream for simplicity with function calls)

type GeminiCandidate struct {
	Content       GeminiContent `json:"content"`
	FinishReason  string        `json:"finishReason"`
}

type GeminiResponse struct {
	Candidates []GeminiCandidate `json:"candidates"`
	Error      *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
}

// Stream response

type GeminiStreamChunk struct {
	Candidates []GeminiCandidate `json:"candidates"`
	Error      *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ─── Internal message history ─────────────────────────────────────────────────

// Для хранения истории используем GeminiContent напрямую

type Session struct {
	Contents  []GeminiContent `json:"contents"`
	System    string          `json:"system"`
	CreatedAt time.Time       `json:"created_at"`
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

// ─── Tool definitions ────────────────────────────────────────────────────────

func getGeminiTools() []GeminiTool {
	return []GeminiTool{
		{
			FunctionDeclarations: []FunctionDecl{
				{
					Name:        "read_file",
					Description: "Read the contents of a file at the given path.",
					Parameters: SchemaNode{
						Type:     "object",
						Required: []string{"path"},
						Properties: map[string]SchemaNode{
							"path": {Type: "string", Description: "Absolute or relative path to the file"},
						},
					},
				},
				{
					Name:        "write_file",
					Description: "Write content to a file, creating it or overwriting if it exists.",
					Parameters: SchemaNode{
						Type:     "object",
						Required: []string{"path", "content"},
						Properties: map[string]SchemaNode{
							"path":    {Type: "string", Description: "Path to the file"},
							"content": {Type: "string", Description: "Content to write"},
						},
					},
				},
				{
					Name:        "append_file",
					Description: "Append content to an existing file.",
					Parameters: SchemaNode{
						Type:     "object",
						Required: []string{"path", "content"},
						Properties: map[string]SchemaNode{
							"path":    {Type: "string", Description: "Path to the file"},
							"content": {Type: "string", Description: "Content to append"},
						},
					},
				},
				{
					Name:        "list_dir",
					Description: "List files and directories at the given path.",
					Parameters: SchemaNode{
						Type:     "object",
						Required: []string{"path"},
						Properties: map[string]SchemaNode{
							"path": {Type: "string", Description: "Directory path to list"},
						},
					},
				},
				{
					Name:        "run_command",
					Description: "Execute a shell command and return stdout+stderr.",
					Parameters: SchemaNode{
						Type:     "object",
						Required: []string{"command"},
						Properties: map[string]SchemaNode{
							"command": {Type: "string", Description: "Shell command to run"},
							"cwd":     {Type: "string", Description: "Working directory (optional)"},
						},
					},
				},
				{
					Name:        "delete_file",
					Description: "Delete a file or empty directory.",
					Parameters: SchemaNode{
						Type:     "object",
						Required: []string{"path"},
						Properties: map[string]SchemaNode{
							"path": {Type: "string", Description: "Path to delete"},
						},
					},
				},
				{
					Name:        "search_files",
					Description: "Search for text pattern in files recursively using grep.",
					Parameters: SchemaNode{
						Type:     "object",
						Required: []string{"pattern", "path"},
						Properties: map[string]SchemaNode{
							"pattern": {Type: "string", Description: "Text or regex to search for"},
							"path":    {Type: "string", Description: "Directory to search in"},
						},
					},
				},
			},
		},
	}
}

// ─── Tool execution ───────────────────────────────────────────────────────────

var dangerousPatterns = []string{
	"rm -rf", "rm -r", "mkfs", "dd if=", ":(){:|:&};:", "chmod -R 777",
	"curl | sh", "wget | sh", "curl | bash", "wget | bash",
	"> /dev/", "fdisk", "parted",
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

func getString(args map[string]interface{}, key string) string {
	if v, ok := args[key]; ok {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

func executeTool(name string, args map[string]interface{}, autoApprove bool) string {
	switch name {
	case "read_file":
		path := getString(args, "path")
		blue.Printf("  📖 read_file: ")
		white.Println(path)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		return string(data)

	case "write_file":
		path := getString(args, "path")
		content := getString(args, "content")
		blue.Printf("  ✏️  write_file: ")
		white.Println(path)
		if !autoApprove {
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
		os.MkdirAll(filepath.Dir(path), 0755)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Sprintf("error: %v", err)
		}
		green.Printf("  ✓ Written %d bytes\n", len(content))
		return fmt.Sprintf("ok: wrote %d bytes to %s", len(content), path)

	case "append_file":
		path := getString(args, "path")
		content := getString(args, "content")
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
		path := getString(args, "path")
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
		command := getString(args, "command")
		cwd := getString(args, "cwd")
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
		for _, line := range strings.Split(strings.TrimRight(result, "\n"), "\n") {
			dim.Printf("     %s\n", line)
		}
		return result

	case "delete_file":
		path := getString(args, "path")
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
		pattern := getString(args, "pattern")
		path := getString(args, "path")
		blue.Printf("  🔍 search_files: ")
		white.Printf("%q in %s\n", pattern, path)
		cmd := exec.Command("grep", "-rn", pattern, path)
		out, _ := cmd.CombinedOutput()
		if len(out) == 0 {
			return "no matches found"
		}
		lines := strings.Split(string(out), "\n")
		if len(lines) > 50 {
			lines = lines[:50]
			lines = append(lines, "... (truncated, showing 50 results)")
		}
		return strings.Join(lines, "\n")

	default:
		return fmt.Sprintf("error: unknown tool %q", name)
	}
}

// ─── API call ─────────────────────────────────────────────────────────────────

func callGemini(cfg Config, contents []GeminiContent, systemPrompt string) (GeminiContent, error) {
	url := fmt.Sprintf("%s/%s:generateContent?key=%s", geminiBaseURL, cfg.Model, cfg.APIKey)

	req := GeminiRequest{
		Contents: contents,
		Tools:    getGeminiTools(),
		GenerationConfig: GenerationConfig{
			MaxOutputTokens: cfg.MaxTokens,
		},
	}

	if systemPrompt != "" {
		req.SystemInstruction = SystemInstruction{
			Parts: []GeminiPart{{Text: systemPrompt}},
		}
	}

	body, err := json.Marshal(req)
	if err != nil {
		return GeminiContent{}, err
	}

	httpReq, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return GeminiContent{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 180 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return GeminiContent{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return GeminiContent{}, err
	}

	var gemResp GeminiResponse
	if err := json.Unmarshal(respBody, &gemResp); err != nil {
		return GeminiContent{}, fmt.Errorf("parse error: %v\nBody: %s", err, string(respBody))
	}

	if gemResp.Error != nil {
		return GeminiContent{}, fmt.Errorf("API error %d: %s", gemResp.Error.Code, gemResp.Error.Message)
	}

	if len(gemResp.Candidates) == 0 {
		return GeminiContent{}, fmt.Errorf("no candidates in response")
	}

	modelContent := gemResp.Candidates[0].Content

	// Печатаем текстовые части
	for _, part := range modelContent.Parts {
		if part.Text != "" {
			fmt.Println()
			magenta.Print("  ◆ ")
			bold.Println("Assistant")
			fmt.Print("  ")
			lines := strings.Split(strings.TrimRight(part.Text, "\n"), "\n")
			for i, line := range lines {
				fmt.Print(line)
				if i < len(lines)-1 {
					fmt.Print("\n  ")
				}
			}
			fmt.Println("\n")
		}
	}

	return modelContent, nil
}

// ─── Agent loop ───────────────────────────────────────────────────────────────

func runAgentLoop(cfg Config, contents []GeminiContent, systemPrompt string) []GeminiContent {
	for {
		modelContent, err := callGemini(cfg, contents, systemPrompt)
		if err != nil {
			red.Printf("  ✗ API error: %v\n\n", err)
			return contents
		}

		// Добавляем ответ модели в историю
		contents = append(contents, modelContent)

		// Ищем function calls
		var functionCalls []GeminiPart
		for _, part := range modelContent.Parts {
			if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part)
			}
		}

		// Нет вызовов — выходим из цикла
		if len(functionCalls) == 0 {
			return contents
		}

		// Выполняем инструменты
		fmt.Println()
		cyan.Println("  ┄ tool calls ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄")

		// Собираем все результаты в одно user-сообщение (требование Gemini API)
		var responseParts []GeminiPart
		for _, part := range functionCalls {
			fc := part.FunctionCall
			fmt.Println()
			result := executeTool(fc.Name, fc.Args, cfg.AutoApprove)
			responseParts = append(responseParts, GeminiPart{
				FunctionResp: &FunctionResp{
					Name:     fc.Name,
					Response: map[string]interface{}{"result": result},
				},
			})
		}

		cyan.Println("\n  ┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄┄")
		fmt.Println()

		// Добавляем результаты как "user" turn (так требует Gemini)
		contents = append(contents, GeminiContent{
			Role:  "user",
			Parts: responseParts,
		})
	}
}

// ─── UI ───────────────────────────────────────────────────────────────────────

func printBanner() {
	fmt.Println()
	cyan.Println("  ╔════════════════════════════════════╗")
	cyan.Print("  ║  ")
	bold.Print("  ⬡  orcli  —  Gemini Agent CLI      ")
	cyan.Println("║")
	cyan.Println("  ╚════════════════════════════════════╝")
	fmt.Println()
}

func printHelp() {
	yellow.Println("  Commands:")
	fmt.Println()
	dim.Println("  /help              show this help")
	dim.Println("  /model <name>      switch model")
	dim.Println("  /auto              toggle auto-approve (off by default)")
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

func buildSystemPrompt(base string) string {
	if base != "" {
		return base
	}
	cwd, _ := os.Getwd()
	return fmt.Sprintf(`You are orcli, a powerful terminal AI agent running on the user's machine.
You have access to tools: read_file, write_file, append_file, list_dir, run_command, delete_file, search_files.
Current working directory: %s
OS: Linux

Be concise. When asked to do something with files or code — do it directly using tools.
Think step by step, use multiple tools when needed, explain what you're doing.`, cwd)
}

func runChat(oneShot string) {
	cfg = loadConfig()
	if cfg.APIKey == "" {
		red.Println("\n  ✗ API key not set!")
		fmt.Println("  Run:    orcli config --key YOUR_KEY")
		fmt.Println("  Or set: export GEMINI_API_KEY=YOUR_KEY\n")
		os.Exit(1)
	}

	systemPrompt := buildSystemPrompt(cfg.SystemPrompt)
	var contents []GeminiContent

	// One-shot mode
	if oneShot != "" {
		contents = append(contents, GeminiContent{
			Role:  "user",
			Parts: []GeminiPart{{Text: oneShot}},
		})
		runAgentLoop(cfg, contents, systemPrompt)
		return
	}

	// Interactive mode
	printBanner()
	green.Printf("  Model:        ")
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
			contents = []GeminiContent{}
			systemPrompt = buildSystemPrompt(cfg.SystemPrompt)
			green.Println("  ✓ Conversation cleared\n")

		case input == "/history":
			if len(contents) == 0 {
				dim.Println("  (empty)\n")
				continue
			}
			for i, c := range contents {
				var preview string
				for _, p := range c.Parts {
					if p.Text != "" {
						preview = p.Text
					} else if p.FunctionCall != nil {
						preview = fmt.Sprintf("[tool call: %s]", p.FunctionCall.Name)
					} else if p.FunctionResp != nil {
						preview = fmt.Sprintf("[tool result: %s]", p.FunctionResp.Name)
					}
				}
				if len(preview) > 80 {
					preview = preview[:80] + "…"
				}
				switch c.Role {
				case "user":
					cyan.Printf("  [%d] user:  ", i)
				case "model":
					magenta.Printf("  [%d] model: ", i)
				}
				dim.Println(preview)
			}
			fmt.Println()

		case input == "/save":
			saveHistory(Session{Contents: contents, System: systemPrompt, CreatedAt: time.Now()})
			green.Println("  ✓ Session saved\n")

		case input == "/load":
			s := loadHistory()
			contents = s.Contents
			if s.System != "" {
				systemPrompt = s.System
			}
			green.Printf("  ✓ Loaded session (%d turns)\n\n", len(contents))

		case input == "/config":
			yellow.Println("  Config:")
			fmt.Printf("  model:        %s\n", cfg.Model)
			fmt.Printf("  max_tokens:   %d\n", cfg.MaxTokens)
			fmt.Printf("  auto_approve: %v\n", cfg.AutoApprove)
			keyPreview := "not set"
			if cfg.APIKey != "" {
				n := len(cfg.APIKey)
				if n > 8 {
					n = 8
				}
				keyPreview = cfg.APIKey[:n] + "..."
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
			systemPrompt = strings.TrimPrefix(input, "/system ")
			green.Println("  ✓ System prompt updated\n")

		default:
			contents = append(contents, GeminiContent{
				Role:  "user",
				Parts: []GeminiPart{{Text: input}},
			})
			contents = runAgentLoop(cfg, contents, systemPrompt)
		}
	}
}

// ─── Cobra CLI ────────────────────────────────────────────────────────────────

func main() {
	var rootCmd = &cobra.Command{
		Use:   "orcli [message]",
		Short: "Gemini Agent CLI — AI with file & shell access",
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

			if key != "" {
				c.APIKey = key
			}
			if model != "" {
				c.Model = model
			}
			if system != "" {
				c.SystemPrompt = system
			}
			if tokens > 0 {
				c.MaxTokens = tokens
			}
			if auto {
				c.AutoApprove = true
			}

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
	configCmd.Flags().String("key", "", "Gemini API key")
	configCmd.Flags().String("model", "", "Model (e.g. gemini-2.5-pro, gemini-2.0-flash)")
	configCmd.Flags().String("system", "", "System prompt")
	configCmd.Flags().Int("max-tokens", 0, "Max output tokens")
	configCmd.Flags().Bool("auto", false, "Enable auto-approve for all tool calls")

	var modelsCmd = &cobra.Command{
		Use:   "models",
		Short: "Show available Gemini models",
		Run: func(cmd *cobra.Command, args []string) {
			yellow.Println("\n  Available Gemini models:\n")
			models := [][]string{
				{"gemini-2.5-pro", "Лучший, огромный контекст (1M токенов)"},
				{"gemini-2.5-flash", "Быстрый и дешёвый, рекомендуется"},
				{"gemini-2.0-flash", "Стабильный, хорошее соотношение цена/качество"},
				{"gemini-2.0-flash-lite", "Самый быстрый и дешёвый"},
				{"gemini-1.5-pro", "Предыдущее поколение Pro"},
				{"gemini-1.5-flash", "Предыдущее поколение Flash"},
			}
			for _, m := range models {
				cyan.Printf("  %-30s", m[0])
				dim.Println(m[1])
			}
			fmt.Println()
			dim.Println("  Ключ: https://aistudio.google.com/apikey")
			dim.Println("  Документация: https://ai.google.dev/gemini-api/docs\n")
		},
	}

	rootCmd.AddCommand(configCmd, modelsCmd)
	rootCmd.Execute()
}
