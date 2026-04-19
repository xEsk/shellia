# Shellia

Shellia is a terminal-native AI shell agent that turns natural language into safe, inspectable command execution.

It is designed for people who want to ask for work in plain language, see exactly what will run, and keep control before anything touches their machine.

## What Shellia is

Shellia is a local CLI tool that:

- understands the current terminal context
- asks an OpenAI-compatible model to propose shell commands
- classifies command risk locally
- shows a plan before execution
- asks for confirmation when needed
- executes commands in the current working directory
- keeps short-term session memory in interactive mode
- can re-plan after inspection steps when later commands depend on earlier output

## What Shellia is not

Shellia is not:

- a fully autonomous DevOps agent
- a remote orchestration tool
- a replacement for reading dangerous commands before approving them
- a guarantee that every generated plan is correct
- a GUI app

It does not try to hide what it is doing. The point is the opposite: make command execution explicit, reviewable, and safer.

## Goal

The goal of Shellia is simple:

1. Let you express intent in natural language.
2. Turn that intent into shell commands that fit your real local context.
3. Keep a strong confirmation and safety layer between the model and your machine.

## Basic features

- Interactive mode and one-shot mode
- Current-context detection:
  - working directory
  - current user
  - operating system
  - current shell
  - Git repository context when available
- OpenAI-compatible `/chat/completions` integration
- Persistent config in `~/.shellia/config.toml`
- Safe/risky/dangerous local command classification
- Per-command confirmation
- Optional auto-run of locally safe commands with `--yes-safe`
- Real-time command output
- Iterative planning when a later step depends on data that still needs to be observed first
- Session memory for follow-ups such as “do the Docker thing from before”

## How it works

Shellia follows this general flow:

1. Detect local context.
2. Send your instruction plus that context to a configurable LLM endpoint.
3. Receive a structured plan.
4. Re-classify every command locally with Shellia's own safety rules.
5. Show the plan.
6. Ask for confirmation when required.
7. Execute commands in the current working directory.
8. If needed, inspect real command output, re-plan, and continue with a better-informed next step.

In interactive mode, Shellia also keeps lightweight session memory so later prompts can refer to previous work.

## Integrations

Shellia supports any provider that exposes an OpenAI-compatible Chat Completions API.

That includes setups such as:

- OpenAI
- Ollama, when exposed through an OpenAI-compatible endpoint
- OpenRouter
- LM Studio
- local proxies or gateways that implement `/chat/completions`

In practice, if you can configure:

- `base_url`
- `api_key`
- `model`

and the endpoint behaves like an OpenAI-compatible chat completions API, Shellia can use it.

## Installation

Shellia is a single Go binary.

### Download a pre-built binary

Download the latest release for your platform from [GitHub Releases](https://github.com/xEsk/shellia/releases), extract it, and move it to your `PATH`:

```bash
# example for macOS Apple Silicon
tar -xzf shellia_v0.1.0_darwin_arm64.tar.gz
mv shellia /usr/local/bin/
```

### Build from source

```bash
go build -o shellia .
```

Then run it:

```bash
./shellia
```

or:

```bash
./shellia "run git status"
```

## Usage

### Interactive mode

Run without arguments:

```bash
./shellia
```

This opens a session where you can keep asking for follow-up actions.

### Manual commands inside interactive mode

Shellia supports two ways to run commands yourself without asking the model to plan them.

When Shellia proposes a command itself, the confirmation prompt also supports:

- `y` to run it as proposed
- `e` to edit the command before running it
- `i` to run that step in interactive terminal mode
- `n` to cancel

By default, Enter does not choose any option. Set `confirmation_default` to `yes`, `no`, `edit`, or `interactive` to make Enter select that action.

#### One direct command with `!`

Prefix a line with `!` to execute it immediately as a manual shell command:

```text
shellia › !pwd
shellia › !cd prova
shellia › !brew update
```

This is useful when you already know exactly what you want to run and do not need the AI to propose anything.
How `!` runs is controlled by `command_mode` in the config:

- `plain`
  - runs as a normal direct command with structured Shellia output
- `interactive`
  - runs in an interactive terminal session and Shellia resumes when it exits

#### Persistent shell mode with `/shell`

Switch the prompt into direct command mode:

```text
shellia › /shell
shell › pwd
shell › cd prova
shell › ls -la
shell › /ai
shellia › where am I now?
```

Commands executed this way still stay inside Shellia's session state:

- the current working directory is preserved
- Git context is refreshed
- command observations can still help later AI prompts
- every command uses the shell engine configured by `shell_mode`

Useful commands in interactive mode:

- `/shell` to enter direct shell mode
- `!<cmd>` to run one direct manual command
- `/ai` to return to AI mode
- `/mode` to show the current mode
- `/clear`, `/context`, `exit`, `/exit`, `/quit`

### One-shot mode

Run with an instruction:

```bash
./shellia "run git status"
```

### One-shot and stay interactive

```bash
./shellia -i "run git status"
```

## Help output

```text
Usage:
  shellia
  shellia [flags] "your instruction here"
  shellia config init
  shellia config path

Flags:
  -api-key: API key
  -base-url: base URL of the OpenAI-compatible API
  -continue-on-error: continue if a command fails
  -debug: show context and debug data
  -i: short alias for --interactive
  -interactive: start or maintain an interactive session
  -model: model to use
  -no-color: disable UI colours
  -raw-response: print the raw model response
  -request-timeout: HTTP request timeout in seconds
  -timeout: per-command timeout in seconds
  -verbose: show full plan and technical detail
  -yes-safe: auto-execute safe commands without confirmation

Config:
  ~/.shellia/config.toml

Examples:
  shellia
  shellia --api-key "YOUR_KEY" "run git status"
  shellia -i "run git status"
  shellia config init
```

## Configuration

Shellia reads persistent settings from:

```text
~/.shellia/config.toml
```

Create it with:

```bash
./shellia config init
```

Show its path with:

```bash
./shellia config path
```

### Example config

```toml
[llm]
base_url = "https://api.openai.com/v1"
model    = "gpt-5.4-mini"
api_key  = ""

[execution]
timeout_seconds         = 120
request_timeout_seconds = 60
yes_safe                = false
continue_on_error       = false
confirmation_default    = "none"
shell_mode              = "interactive"
command_mode            = "plain"

[output]
capture_stdout_bytes     = 131072
capture_stderr_bytes     = 262144
observation_output_chars = 1200
summary_output_chars     = 4000

[ui]
verbose            = false
no_color           = false
show_system_output = true
show_command_popup = true
```

### Configuration precedence

Shellia applies settings in this order:

1. built-in defaults
2. `~/.shellia/config.toml`
3. environment variables
4. CLI flags

Supported environment variables:

- `SHELLIA_BASE_URL`
- `SHELLIA_MODEL`
- `SHELLIA_API_KEY`
- `SHELLIA_SHELL_MODE`
- `SHELLIA_COMMAND_MODE`

Compatibility fallback variables:

- `OPENAI_BASE_URL`
- `OPENAI_MODEL`
- `OPENAI_API_KEY`

### UI controls

- `show_command_popup`
  - shows the slash-command popup while typing `/` when set to `true`
- `show_system_output`
  - shows live `system output` blocks in the terminal when set to `true`; captured output is still kept for planning and summaries when set to `false`
- `no_color`
  - disables ANSI colours
- `verbose`
  - shows extra technical details in plans

### Output capture controls

Shellia streams command output live to the terminal, but it also keeps a bounded in-memory capture so later planning and summarization do not send huge logs to the model.

These settings control that behavior:

- `capture_stdout_bytes`
  - how many bytes of `stdout` Shellia keeps per command
- `capture_stderr_bytes`
  - how many bytes of `stderr` Shellia keeps per command
- `observation_output_chars`
  - how much captured output is sent back to the model during iterative planning
- `summary_output_chars`
  - how much captured output is sent to the model for the final answer

If output is truncated, Shellia marks it explicitly instead of pretending it captured everything.

### Command engine modes

Shellia lets you choose how manual commands are executed.

- `shell_mode`
  - controls how commands run inside `/shell`
- `command_mode`
  - controls how one-off `!<cmd>` commands run
- `confirmation_default`
  - controls what Enter selects in AI command confirmation prompts

Allowed command engine values:

- `plain`
  - normal command execution with Shellia's structured output
- `interactive`
  - run inside an interactive terminal session and return control to Shellia when the command exits

Allowed confirmation defaults:

- `none`
  - Enter does not choose anything; type `y`, `e`, `i`, or `n`
- `yes`, `no`, `edit`, `interactive`
  - Enter selects that confirmation action

Defaults:

- `shell_mode = "interactive"`
- `command_mode = "plain"`
- `confirmation_default = "none"`

## Safety model

Shellia does not blindly trust the model.

It applies a local safety layer that classifies commands as:

- `safe`
- `risky`
- `dangerous`

Examples:

- Safe:
  - `ls`
  - `pwd`
  - `cat`
  - `git status`
  - read-only Docker inspection commands
- Risky:
  - filesystem changes
  - `git pull`
  - many Docker operations
- Dangerous:
  - `sudo`
  - user or system modifications
  - destructive commands such as `rm`

Commands that are not clearly safe require confirmation.

With `--yes-safe`, Shellia auto-runs only commands that its own local classifier considers safe.

## What Shellia keeps in session memory

In interactive mode, Shellia keeps a lightweight memory of:

- the pending task you are working on
- recently created files
- recent runtime hints such as Docker or PHP context
- the last referenced file

This helps with follow-up prompts such as:

- “do the Docker thing from before”
- “run the PHP file now”
- “try again”

## Examples

List files:

```bash
./shellia "list the files in this directory"
```

Check git state:

```bash
./shellia "show me the git status"
```

Run in interactive mode:

```bash
./shellia
```

Use a custom provider:

```bash
./shellia \
  --base-url "http://localhost:11434/v1" \
  --api-key "ollama" \
  --model "llama3.1" \
  "show me the files in this directory"
```

## Limitations

Shellia is intentionally conservative, but that does not make it infallible.

Current practical limits:

- It depends on the quality of the configured model.
- It is strongest for local shell work, not distributed orchestration.
- It may still need extra context for ambiguous requests.
- You should still review risky commands before approving them.

## License

Shellia is licensed under the MPL-2.0 license. See [LICENSE](LICENSE).
