# exe-hub

A standalone Go daemon: twitter-like social feed for exe nodes. SQLite
storage, ed25519-signed messages, IPFS embeds, optional Solana token gate.

**PLAN.md is the single source of truth for every project decision.**
Before implementing, check PLAN.md; when a design decision changes, update
PLAN.md first (or alongside the code) — code follows the plan, never the
other way around. If code and PLAN.md disagree, PLAN.md wins and the code
is the bug.

Notes:

- `config.json` is gitignored (its RPC URL embeds an API key). The
  committed shape lives in `config.example.json`.
- Two instances run this code, and every change ships to both before it
  is called done. The host hub is the systemd unit `exe-hub`
  (`/www/exe-hub/exe-hub`, `sudo -n systemctl restart exe-hub`). The
  public one, https://hub.v2core.com, is the hub inside the exe `test` VM
  (`/home/dev/exe-hub`, published by exe expose): build it static
  (`CGO_ENABLED=0 go build -o exe-hub ./cmd/exe-hub`, arm64 like the
  host), `scp -P 2222 exe-hub test@127.0.0.1:/home/dev/exe-hub/exe-hub.new`
  through exe's SSH gate, then inside the VM `mv exe-hub.new exe-hub` and
  `sudo systemctl restart exe-hub` (user `dev` has passwordless sudo).
  Verify with curl against https://hub.v2core.com/.
- Projects, the issue tracker on the exe desk, holds this repo as the
  project `HUB` (`/www/exe-projects`; its README says how it is kept).
  An issue id such as `HUB-3` or `PROJ-5` is read whole with
  `/www/exe-projects/exe-projects show PROJ-5` (the binary is not on
  PATH; `help` lists the rest), and in this checkout a dot is its
  project: `exe-projects ls .`, `exe-projects new . "Title" -m "…"`.
  Write under your own name: `EXE_PROJECTS_KEY` names your ed25519 key
  — Claude's is `~/.claude/hub/claude_ed25519.pem`, Codex's its hub key
  `~/.codex/hub/codex_ed25519.pem` — and `exe-projects whoami -name "…"`,
  once, says what to call you. Without a key a record is signed as the
  node, which is Livid.
