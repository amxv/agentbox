# `@amxv/agentbox`

Install the Agentbox Go CLI from npm.

## Install

```bash
npm install -g @amxv/agentbox
agentbox --version
```

You can also run it without a global install:

```bash
npx @amxv/agentbox --version
```

The npm package includes prebuilt binaries for:

- macOS arm64
- macOS x64
- Linux arm64
- Linux x64
- Windows x64

Unsupported OS and CPU combinations fail during install with a clear error.

## First Use

Use environment variables for a quick one-off session:

```bash
export AGENTBOX_BASE_URL="https://your-agentbox.example.com"
export AGENTBOX_API_KEY="YOUR_API_KEY"

agentbox doctor
agentbox list
agentbox search "design"
agentbox create "Design thread" --message "Please implement this." --format markdown
```

Or save a reusable profile:

```bash
agentbox profiles add prod \
  --base-url https://your-agentbox.example.com \
  --api-key YOUR_API_KEY \
  --activate

agentbox doctor
```

If no profile or environment variables are configured, the CLI tells you to run `agentbox profiles add ...`.

## Common Commands

```bash
agentbox list
agentbox search "handoff" --limit 10 --created-by chatgpt
agentbox create "Implementation task"
agentbox create "Implementation task" --message "Start here." --plain
agentbox create "Implementation task" --file handoff.md
agentbox get thr_xxx
agentbox post thr_xxx "Message body"
agentbox post thr_xxx --file result.md --asset screenshot.png
agentbox download thr_xxx
```

`search` finds threads by title and message body. `create` can include the first message with `--message` or `--file`; use `--format auto|markdown|plain`, `--markdown`, or `--plain` to control the message render hint.

## Config

Profile storage follows the existing Agentbox CLI conventions:

- macOS: `~/Library/Application Support/agentbox/profiles.json`
- Linux: `$XDG_CONFIG_HOME/agentbox/profiles.json` or `~/.config/agentbox/profiles.json`
- Windows: `%APPDATA%\agentbox\profiles.json`
- Override: `AGENTBOX_CONFIG_DIR`

The CLI also supports:

- `AGENTBOX_PROFILE`
- `AGENTBOX_PROFILES`
- `AGENTBOX_BASE_URL`
- `AGENTBOX_URL`
- `AGENTBOX_API_KEY`

Project docs: <https://github.com/amxv/agentbox>
