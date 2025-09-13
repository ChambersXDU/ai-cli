ai-cli — A tiny OpenAI-compatible CLI assistant

![CI](https://github.com/ChambersXDU/ai-cli/actions/workflows/ci.yml/badge.svg?branch=main)

Quick start

Build:

```bash
go build -o ai-cli .
```

Run:

```bash
./ai-cli "What is the best way to..."
# or via stdin
echo "ls -la" | ./ai-cli
```

Configuration

The tool uses a config file at `~/.ai_cli_config`. On first run the tool will create a template and exit; edit that file and set `api_key` before running again.

Important fields in `~/.ai_cli_config`:
- `api_key` (REQUIRED)
- `base_url` (default: https://api.openai.com/v1)
- `default_model` and `models` (comma-separated list)
- `system_prompt`, `request_timeout`, `proxy_url`

Notes

- The program streams responses from OpenAI-compatible APIs and prints plain text to stdout.
- The default config creation exits the program so you can safely set your API key.

License: MIT
