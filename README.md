# Shellia

Shellia is a terminal-native AI shell agent that turns natural language into safe, inspectable command execution.

It is designed for people who want to ask for work in plain language, see exactly what will run, and keep control before anything touches their machine.

## What Shellia is

Shellia is a local CLI tool that:

- understands the current terminal context
- asks an OpenAI-compatible model to decide whether to execute, complete, or report a blocker
- classifies command risk locally
- shows a plan before execution
- asks for confirmation when needed
- executes commands in the current working directory
- keeps short-term session memory in interactive mode
- re-evaluates the objective after every command batch until it is complete or blocked

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
- OpenAI-compatible `/chat/completions` integration
- Persistent config in `~/.config/shellia/config.toml`
- Safe/risky/dangerous local command classification
- Per-command confirmation
- Optional auto-run of locally safe commands with `--yes-safe`
- Real-time command output
- Intent-aware decisions for actions, local observations, capability questions, and explanations
- Causal completion checks that prevent a requested change from finishing as a mere explanation
- Iterative planning when a later step depends on data that still needs to be observed first
- Failure-aware replanning: ordinary command failures become bounded observations, while dependent later steps are skipped
- Session memory for follow-ups such as “do the Docker thing from before”

## How it works

Shellia follows this general flow:

1. Detect local context.
2. Send your instruction plus that context to a configurable LLM endpoint.
3. Receive one structured decision: execute, complete, or blocked.
4. Lock the objective as an action, observation, capability question, or explanation.
5. For execution decisions, re-classify every command locally with Shellia's own safety rules.
6. Show the plan and command purposes.
7. Ask for confirmation when required.
8. Execute commands in the current working directory.
9. Feed bounded evidence back into the same workflow and continue until causal evidence supports completion or a real blocker is reached.

Requests for a current local value, such as “how much disk space is free?”, are observations and use the terminal. Explicit capability questions, such as “can you check how much disk space is free?”, do not execute immediately: Shellia explains the approach and can offer a structured follow-up objective. An unequivocal “yes” starts that objective as a new workflow with the same visible plan and safety confirmations as any other task.

`--plan` and `/plan` use the same planning contract but have no execution authority: they show the first useful plan and never invoke the executor.

In interactive mode, Shellia also keeps lightweight session memory so later prompts can refer to previous work.

Git repository state is not collected or sent as ambient context. When a task depends on Git, Shellia plans an explicit inspection command such as `git status --short` and feeds its bounded captured output into the next planning round.

## Integrations

Shellia supports any provider that exposes an OpenAI-compatible Chat Completions API.

That includes setups such as:

- OpenAI
- Ollama, when exposed through an OpenAI-compatible endpoint
- OpenRouter
- LM Studio
- MLX Server
- llama.cpp through `llama-server`
- local proxies or gateways that implement `/chat/completions`

In practice, if you can configure:

- `base_url`
- `model`
- `api_key`, unless it is a local loopback endpoint that does not require one

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
go build -o shellia ./cmd/shellia
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
- command observations can still help later AI prompts
- every command uses the shell engine configured by `shell_mode`

Useful commands in interactive mode:

- `/shell` to enter direct shell mode
- `!<cmd>` to run one direct manual command
- `/ai` to return to AI mode
- `/mode` to show the current mode
- `/model` to list configured model profiles
- `/model <name>` to switch model profile and persist it as `default_model`
- `/theme` to list the four visual themes and mark the active one
- `/theme <plain|guide|bands|cards>` to switch theme immediately and persist it as `ui.style`
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
  -model-name: configured model profile to use
  -no-color: disable UI colours
  -plan: show the command plan without executing it
  -raw-prompt: print the raw model prompts
  -raw-response: print the raw model response
  -request-timeout: HTTP request timeout in seconds
  -timeout: per-command timeout in seconds
  -trace: write a JSONL diagnostic trace for this session
  -trace-dir: directory for JSONL diagnostic trace files
  -verbose: show full plan and technical detail
  -yes-safe: auto-execute safe commands without confirmation

Config:
  ~/.config/shellia/config.toml

Examples:
  shellia
  shellia --api-key "YOUR_KEY" "run git status"
  shellia -i "run git status"
  shellia config init
```

## Configuration

Shellia reads persistent settings from:

```text
~/.config/shellia/config.toml
```

Create it with:

```bash
./shellia config init
```

Show its path with:

```bash
./shellia config path
```

`~/.config/shellia/config.toml` is the recommended location and the path created by `shellia config init`. If `XDG_CONFIG_HOME` is set, Shellia uses `$XDG_CONFIG_HOME/shellia/config.toml` instead. For compatibility, Shellia also reads the old `~/.shellia/config.toml` path when the recommended file does not exist.

### Example config

```toml
default_model = "openai"

[[models]]
name = "openai"
base_url = "https://api.openai.com/v1"
model = "gpt-5.4-mini"
api_key_env = "SHELLIA_API_KEY"
supports_response_format = true

[[models]]
name = "llama-cpp"
base_url = "http://localhost:8080/v1"
model = "unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF:UD-Q4_K_XL"
api_key = ""
supports_response_format = true

[[models]]
name = "mlx"
base_url = "http://localhost:8080/v1"
model = "mlx-community/Qwen3-Coder-30B-A3B-Instruct-4bit"
api_key = ""
supports_response_format = false

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

[ui]
style              = "plain" # plain | guide | bands | cards
prompt_identity    = "user"  # user | you
verbose            = false
no_color           = false
show_system_output = true
show_command_popup = true

[trace]
enabled = false
dir     = ""
```

### Model profiles

Define one or more `[[models]]` entries. Shellia selects the active profile in this order:

1. `--model-name`
2. `SHELLIA_MODEL_NAME`
3. `default_model`
4. the first configured model

`--base-url`, `--model`, and `--api-key` are one-shot overrides over the selected profile.

Use `supports_response_format = false` for endpoints that do not support OpenAI's `response_format` parameter. If omitted, Shellia assumes `true`. The official `mlx_lm.server` should normally use `supports_response_format = false`; OpenAI and llama.cpp can use the default.

### Configuration precedence

Shellia applies settings in this order:

1. built-in defaults
2. selected `[[models]]` profile from `~/.config/shellia/config.toml`
3. environment variables
4. CLI flags

Supported environment variables:

- `SHELLIA_MODEL_NAME`
- `SHELLIA_BASE_URL`
- `SHELLIA_MODEL`
- `SHELLIA_API_KEY`
- `SHELLIA_SHELL_MODE`
- `SHELLIA_COMMAND_MODE`
- `SHELLIA_PLANNING_MAX_ROUNDS`

Compatibility fallback variables:

- `OPENAI_BASE_URL`
- `OPENAI_MODEL`
- `OPENAI_API_KEY`

### UI controls

- `style`
  - selects the terminal structure: `plain`, `guide`, `bands`, or `cards`
  - defaults to `plain`, Shellia's compact original structure
  - each style uses a fixed built-in colour theme; colours are not configurable
- `prompt_identity`
  - `user` shows the active terminal username, such as `xesc ›` or `root ›`
  - `you` uses the stable generic label `you ›`
  - defaults to `user`; if Shellia cannot detect a username, it falls back to `you`
  - applies consistently to the live prompt and the submitted user block in every visual theme
- `show_command_popup`
  - shows the slash-command popup while typing `/` when set to `true`
- `show_system_output`
  - shows live `system output` blocks in the terminal when set to `true`; captured output is still kept as bounded workflow evidence when set to `false`
- `no_color`
  - disables Shellia-generated ANSI colours while preserving the selected style's rails, markers, borders, spacing, and indentation
- `verbose`
  - shows extra technical details in plans

The `--no-color` flag applies the same no-ANSI behavior for one run. When stdout is piped or redirected, or when `TERM=dumb`, Shellia automatically uses `plain` output without ANSI even if another style is configured. Output produced by a child process running in an interactive PTY is passed through unchanged: Shellia does not filter or rewrite the child's ANSI sequences.

The four visual themes present the same plans, confirmations, command output, and Markdown answers with different hierarchy:

| Theme | Visual structure |
| --- | --- |
| `plain` | Compact transcript with the original separators and command boxes. |
| `guide` | Thick actor rails with technical execution nested under a thinner rail. |
| `bands` | Full-width user, Shellia, plan, execution, and answer bands. |
| `cards` | Rounded actor cards with a nested execution surface. |

Inside an interactive session, `/theme` shows the available themes and `/theme <name>` applies and persists the selection without restarting Shellia. `--no-color` removes only colour: the selected theme keeps its rails, bands, borders, spacing, and indentation.

### Output capture controls

Shellia streams command output live to the terminal, but it also keeps a bounded in-memory capture so later workflow decisions do not send huge logs to the model.

These settings control that behavior:

- `capture_stdout_bytes`
  - how many bytes of `stdout` Shellia keeps per command
- `capture_stderr_bytes`
  - how many bytes of `stderr` Shellia keeps per command
- `observation_output_chars`
  - the shared budget for captured output sent back to the workflow model

If output is truncated, Shellia marks it explicitly instead of pretending it captured everything.

### Session trace diagnostics

Shellia can write one JSONL diagnostic file per session. This is useful when you want to inspect exactly what Shellia sent to the model, what the model returned, which internal decision Shellia made, and which commands were confirmed or executed.

Enable it for one run:

```bash
./shellia --trace "show me the git status"
```

Use a custom trace directory:

```bash
./shellia --trace --trace-dir /tmp/shellia-traces "show me the git status"
```

Or enable it persistently:

```toml
[trace]
enabled = true
dir = ""
```

When `dir` is empty, Shellia writes traces to `$XDG_STATE_HOME/shellia/sessions` or `~/.local/state/shellia/sessions`. Trace files include prompts, raw model responses, planner decisions, command confirmations, executed commands, exit codes, and the command output that Shellia captured locally.

### Planning controls

- `planning_max_rounds`
  - maximum number of planning follow-up rounds before Shellia asks whether to continue
  - can be overridden for one run with `SHELLIA_PLANNING_MAX_ROUNDS`
- `continue_on_error`
  - when `false`, the first failed or timed-out command stops the current batch
  - when `true`, Shellia skips dependent later commands and runs only commands the plan explicitly marks independent of earlier failures
  - ordinary failures trigger bounded replanning; timeouts, cancellations, `!`, and `/shell` do not

Recovery plans retain all normal plan and command confirmations, including the existing `--yes-safe` rules.

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
- a pending executable objective offered after a capability question

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

Use MLX Server:

```bash
mlx_lm.server --model mlx-community/Qwen3-Coder-30B-A3B-Instruct-4bit

./shellia \
  --base-url "http://localhost:8080/v1" \
  --model "mlx-community/Qwen3-Coder-30B-A3B-Instruct-4bit" \
  "show me the files in this directory"
```

For the official MLX LM Server config profile, set `supports_response_format = false`.

Use llama.cpp:

```bash
llama-server -hf unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF:UD-Q4_K_XL --port 8080

./shellia \
  --base-url "http://localhost:8080/v1" \
  --model "unsloth/Qwen3-Coder-30B-A3B-Instruct-GGUF:UD-Q4_K_XL" \
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
