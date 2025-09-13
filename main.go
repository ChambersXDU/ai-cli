package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Config 结构体用于存储配置
type Config struct {
	APIKey         string
	BaseURL        string
	DefaultModel   string
	Models         []string
	RequestTimeout int
	SystemPrompt   string
	ProxyURL       string
}

// API 请求和响应的结构体
type APIRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type StreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

const (
	defaultSystemPrompt = `You are a Linux CLI assistant for technical user. Provide concise answers and commands. Be direct, no filler. Plain text style (No markdwon). Put commands on their own lines for easy copying. Add short comments to commands if needed. Prefer single-line commands. But be alert to dangerous commands.`
)

// detectSystemPrompt 根据运行平台自动生成更合适的 system prompt（尽量识别 Linux 发行版）
func detectSystemPrompt() string {
	switch runtime.GOOS {
	case "darwin":
		return `You are a macOS CLI assistant for technical user. Provide concise answers and commands. Be direct, no filler. Plain text style. Put commands on their own lines for easy copying. Add short comments to commands if needed. Prefer single-line commands. Be alert to dangerous commands.`
	case "linux":
		if b, err := os.ReadFile("/etc/os-release"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(l, "PRETTY_NAME=") {
					return fmt.Sprintf("You are a %s CLI assistant for technical user. Provide concise answers and commands. Be direct, no filler. Plain text style. Put commands on their own lines for easy copying. Add short comments to commands if needed. Prefer single-line commands. Be alert to dangerous commands.", strings.Trim(l[len("PRETTY_NAME="):], `"`))
				}
			}
		}
		return fmt.Sprintf("You are a Linux CLI assistant for technical user. Provide concise answers and commands. Be direct, no filler. Plain text style. Put commands on their own lines for easy copying. Add short comments to commands if needed. Prefer single-line commands. Be alert to dangerous commands.")
	case "windows":
		shell := "PowerShell"
		if com := strings.ToLower(os.Getenv("COMSPEC")); strings.Contains(com, "cmd") {
			shell = "cmd"
		}
		return fmt.Sprintf("You are a Windows (%s) CLI assistant for technical user. Provide concise answers and commands. Be direct, no filler. Use %s-style commands where appropriate. Put commands on their own lines for easy copying.", shell, shell)
	default:
		return defaultSystemPrompt
	}
}

var (
	cfg        Config
	configName = ".ai_cli_config"
)

// rootCmd 代表了应用的基础命令
var rootCmd = &cobra.Command{
	Use:   "ai-cli [prompt]",
	Short: "A command-line AI assistant",
	Long: `A fast and simple command-line AI assistant that connects to OpenAI-compatible APIs.
It reads your prompt from the command line arguments or from standard input (stdin).`,
	Args:  cobra.ArbitraryArgs,
	Run: func(cmd *cobra.Command, args []string) {
		// 1. 加载配置
		if err := loadConfig(); err != nil {
			log.Fatalf("Error loading config: %v", err)
		}

		// 2. 处理命令行标志覆盖
		if modelFlag, _ := cmd.Flags().GetString("model"); modelFlag != "" {
			cfg.DefaultModel = modelFlag
		}

		// 3. 获取用户输入
		var prompt string
		if len(args) > 0 {
			prompt = strings.Join(args, " ")
		} else {
			// 如果没有命令行参数，则从 stdin 读取
			stat, _ := os.Stdin.Stat()
			if (stat.Mode() & os.ModeCharDevice) == 0 {
				stdinBytes, err := io.ReadAll(os.Stdin)
				if err != nil {
					log.Fatalf("Failed to read from stdin: %v", err)
				}
				prompt = strings.TrimSpace(string(stdinBytes))
			}
		}

		if prompt == "" {
			cmd.Help()
			return
		}

		// 4. 执行 API 调用
		callAPI(prompt)
	},
}

func init() {
	// 定义命令行标志
	rootCmd.PersistentFlags().StringP("model", "m", "", "Specify the model to use (overrides config default)")

	// 添加 model、add、remove 子命令
	rootCmd.AddCommand(modelCmd, addCmd, removeCmd)
}

// modelCmd: 交互式选择或通过参数设置默认模型
var modelCmd = &cobra.Command{
	Use:   "model [name]",
	Short: "从配置中的模型列表选择或设置默认模型",
	Long:  "从配置中的模型列表选择一个作为默认模型。无参数时进入交互选择；提供模型名则直接设置（必须存在于配置中）。",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := loadConfig(); err != nil {
			log.Fatalf("Error loading config: %v", err)
		}

		if len(cfg.Models) == 0 {
			fmt.Fprintln(os.Stderr, "没有可用的模型，请在配置文件中添加模型。")
			return
		}

		// 直接设置模式： ai-cli model <name>
		if len(args) == 1 {
			name := args[0]
			found := false
			for _, m := range cfg.Models {
				if m == name {
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "模型 '%s' 未在配置中找到。可用模型：%s\n", name, strings.Join(cfg.Models, ", "))
				return
			}

			cfg.DefaultModel = name
			if err := saveConfig(); err != nil {
				log.Fatalf("Failed to save config: %v", err)
			}
			fmt.Printf("默认模型已设置为 '%s'\n", name)
			return
		}

		// 交互式选择
		fmt.Println("可用模型：")
		for i, m := range cfg.Models {
			mark := " "
			if m == cfg.DefaultModel {
				mark = "*"
			}
			fmt.Printf("[%d] %s %s\n", i+1, m, mark)
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("输入编号选择默认模型（当前：%s），回车取消： ", cfg.DefaultModel)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Println("已取消。")
			return
		}

		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(cfg.Models) {
			fmt.Fprintln(os.Stderr, "无效的选择。")
			return
		}

		cfg.DefaultModel = cfg.Models[idx-1]
		if err := saveConfig(); err != nil {
			log.Fatalf("Failed to save config: %v", err)
		}
		fmt.Printf("默认模型已设置为 '%s'\n", cfg.DefaultModel)
	},
}

// 新增模型命令: ai-cli add <model>
var addCmd = &cobra.Command{
	Use:   "add [model]",
	Short: "添加一个模型到配置列表",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := loadConfig(); err != nil {
			log.Fatalf("Error loading config: %v", err)
		}
		name := strings.TrimSpace(args[0])
		if name == "" {
			fmt.Fprintln(os.Stderr, "模型名不能为空")
			return
		}
		for _, m := range cfg.Models {
			if m == name {
				fmt.Fprintf(os.Stderr, "模型 '%s' 已存在。\n", name)
				return
			}
		}
		cfg.Models = append(cfg.Models, name)
		if cfg.DefaultModel == "" {
			cfg.DefaultModel = name
		}
		if err := saveConfig(); err != nil {
			log.Fatalf("Failed to save config: %v", err)
		}
		fmt.Printf("模型 '%s' 已添加。\n", name)
	},
}

// 删除模型命令: ai-cli remove（交互编号选择）
var removeCmd = &cobra.Command{
	Use:   "remove",
	Short: "从配置中删除模型（交互选择编号）",
	Run: func(cmd *cobra.Command, args []string) {
		if err := loadConfig(); err != nil {
			log.Fatalf("Error loading config: %v", err)
		}
		if len(cfg.Models) == 0 {
			fmt.Fprintln(os.Stderr, "没有可删除的模型。")
			return
		}
		fmt.Println("可用模型：")
		for i, m := range cfg.Models {
			mark := " "
			if m == cfg.DefaultModel {
				mark = "*"
			}
			fmt.Printf("[%d] %s %s\n", i+1, m, mark)
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Print("输入编号删除模型，回车取消： ")
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
		if input == "" {
			fmt.Println("已取消。")
			return
		}
		idx, err := strconv.Atoi(input)
		if err != nil || idx < 1 || idx > len(cfg.Models) {
			fmt.Fprintln(os.Stderr, "无效的选择。")
			return
		}
		removed := cfg.Models[idx-1]
		// 从切片中移除
		cfg.Models = append(cfg.Models[:idx-1], cfg.Models[idx:]...)
		// 如果删除的是默认模型，重置为第一个或清空
		if cfg.DefaultModel == removed {
			if len(cfg.Models) > 0 {
				cfg.DefaultModel = cfg.Models[0]
			} else {
				cfg.DefaultModel = ""
			}
		}
		if err := saveConfig(); err != nil {
			log.Fatalf("Failed to save config: %v", err)
		}
		fmt.Printf("模型 '%s' 已删除。\n", removed)
	},
}

// configFilePath 返回配置文件绝对路径
func configFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("could not determine user home directory: %w", err)
	}
	return filepath.Join(home, configName), nil
}
// saveConfig 将当前 cfg 中的 default_model 和 models 写回配置文件（保持其它行不变）
func saveConfig() error {
	path, err := configFilePath()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	s := string(b)
	if strings.Contains(s, "default_model") {
		s = replaceLine(s, "default_model", fmt.Sprintf("default_model = %s", cfg.DefaultModel))
	} else {
		s += "\n" + fmt.Sprintf("default_model = %s", cfg.DefaultModel)
	}
	if strings.Contains(s, "models") {
		s = replaceLine(s, "models", fmt.Sprintf("models = %s", strings.Join(cfg.Models, ", ")))
	} else {
		s += "\n" + fmt.Sprintf("models = %s", strings.Join(cfg.Models, ", "))
	}
	return os.WriteFile(path, []byte(s), 0644)
}

// replaceLine 替换以 key 开头的行内容
func replaceLine(s, key, newLine string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), key) {
			out = append(out, newLine)
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}

func loadConfig() error {
	path, err := configFilePath()
	if err != nil {
		return err
	}
	// 如果不存在，创建默认并退出(保持原行为)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return createDefaultConfig(path)
	}

	// 默认值
	cfg = Config{BaseURL: "https://api.openai.com/v1", DefaultModel: "gpt-4o-mini", Models: []string{"gpt-4o-mini"}, RequestTimeout: 30, SystemPrompt: detectSystemPrompt()}

	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	s, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(s), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.TrimSpace(parts[0])
		v := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		switch k {
		case "api_key":
			cfg.APIKey = v
		case "base_url":
			cfg.BaseURL = v
		case "default_model":
			cfg.DefaultModel = v
		case "models":
			if v != "" {
				var ms []string
				for _, m := range strings.Split(v, ",") {
					if t := strings.TrimSpace(m); t != "" {
						ms = append(ms, t)
					}
				}
				if len(ms) > 0 {
					cfg.Models = ms
				}
			}
		case "request_timeout":
			if t, e := strconv.Atoi(v); e == nil {
				cfg.RequestTimeout = t
			}
		case "system_prompt":
			cfg.SystemPrompt = v
		case "proxy_url":
			cfg.ProxyURL = v
		}
	}

	if cfg.APIKey == "" || cfg.APIKey == "YOUR_API_KEY_HERE" {
		return fmt.Errorf("API key is missing or not set in %s. Please edit the file and set your API key", path)
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	if len(cfg.Models) == 0 {
		cfg.Models = []string{cfg.DefaultModel}
	}
	return nil
}

func createDefaultConfig(path string) error {
	fmt.Fprintf(os.Stderr, "Configuration file not found. Creating default config at: %s\n", path)
	cfg := fmt.Sprintf(`# Configuration for the AI CLI Tool

# Your API Key (REQUIRED)
api_key = YOUR_API_KEY_HERE

# Base URL for the OpenAI-compatible API
base_url = https://api.openai.com/v1

# Default model to use if -m is not specified
default_model = gpt-4o-mini

# Comma-separated list of available models you want to use
models = gpt-4o-mini, gpt-4.1-nano, gpt-4.1-mini

# Request timeout in seconds
request_timeout = 30

# System prompt to guide the AI's behavior
system_prompt = %s

# Optional: Specify a proxy URL if needed (e.g., http://127.0.0.1:7890)
# Leave blank if you don't need a proxy
proxy_url = 
`, detectSystemPrompt())
	if err := os.WriteFile(path, []byte(cfg), 0644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "\nIMPORTANT: Please edit '%s' to add your API key and customize settings. Then run the command again.\n", path)
	os.Exit(1)
	return nil
}

func callAPI(prompt string) {
	// 构建消息
	messages := []Message{}
	if cfg.SystemPrompt != "" {
		messages = append(messages, Message{Role: "system", Content: cfg.SystemPrompt})
	}
	messages = append(messages, Message{Role: "user", Content: prompt})

	// 构建请求体
	reqBody := APIRequest{
		Model:    cfg.DefaultModel,
		Messages: messages,
		Stream:   true,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		log.Fatalf("Error marshalling request: %v", err)
	}

	// 创建 HTTP 客户端，并配置代理（如果需要）
	httpClient := &http.Client{
		Timeout: time.Duration(cfg.RequestTimeout) * time.Second,
	}
	if cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			log.Fatalf("Invalid proxy_url: %v", err)
		}
		httpClient.Transport = &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}
	}

	// 创建请求
	req, err := http.NewRequest("POST", cfg.BaseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		log.Fatalf("Error creating request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	// 发送请求
	resp, err := httpClient.Do(req)
	if err != nil {
		log.Fatalf("Error making request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Fatalf("API request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// 处理流式响应
	scanner := bufio.NewScanner(resp.Body)
	// increase buffer to support long SSE lines (tokens) from some providers
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024) // up to 10MB
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				break
			}

			var streamResp StreamResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				// 忽略无法解析的行
				continue
			}

			if len(streamResp.Choices) > 0 {
				fmt.Print(streamResp.Choices[0].Delta.Content)
			}
		}
	}
	fmt.Println() // 在结束后打印一个换行符

	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading stream: %v", err)
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
