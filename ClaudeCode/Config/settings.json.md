

``` json
{
  "permissions": {
    "allow": [
      "Bash(*)",
      "Read",
      "Write",
      "Edit",
      "MultiEdit",
      "Glob",
      "Grep",
      "LS",
      "WebFetch",
      "WebSearch",
      "TodoWrite",
      "Task",
      "Bash(rm -rf*)",
      "mcp__engram__*",
      "mcp__gentleman-book__*"
    ],
    "deny": [
      "Bash(git commit:*)",
      "Bash(git push:*)",
      "Bash(git rebase:*)",
      "Bash(git reset --hard:*)",
      "Bash(sudo:*)",
      "Bash(su:*)",
      "Read(**/.env)",
      "Read(**/.env.*)",
      "Read(**/*.key)",
      "Read(**/*.pem)",
      "Read(**/secrets/**)"
    ]
  },
  "model": "sonnet",
  "enabledPlugins": {
    "rust-analyzer-lsp@claude-plugins-official": true,
    "frontend-design@claude-plugins-official": true,
    "figma@claude-plugins-official": true,
    "engram@engram": true,
    "gopls-lsp@claude-plugins-official": true
  },
  "extraKnownMarketplaces": {
    "claude-code-plugins": {
      "source": {
        "source": "github",
        "repo": "anthropics/claude-code"
      }
    },
    "engram": {
      "source": {
        "source": "github",
        "repo": "Gentleman-Programming/engram"
      }
    }
  },
  "effortLevel": "low"
}

```