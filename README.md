# local-llm

Personal **OpenAI-compatible API** for local models on an Apple Silicon Mac.

A small Go gateway listens on your LAN with API-key auth and friendly model aliases. **Ollama** runs inference on loopback only (no LAN bind, no built-in auth).

```
[devices on home Wi‑Fi/Ethernet]
        │  http://<mac-ip>:4000/v1
        │  Authorization: Bearer <key>
        ▼
   local-llm (Go)  ──►  Ollama 127.0.0.1:11434
```

Cost: **$0** (Homebrew, Go, Ollama, open weights).

## Quick start

```bash
./scripts/setup.sh   # install Ollama if needed, build, pull tiny model, create .env
make run             # start gateway on 0.0.0.0:4000
```

In another terminal (key is in `.env`):

```bash
set -a && source .env && set +a

curl http://127.0.0.1:4000/v1/models \
  -H "Authorization: Bearer $LOCAL_LLM_API_KEY"

curl http://127.0.0.1:4000/v1/chat/completions \
  -H "Authorization: Bearer $LOCAL_LLM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"tiny","messages":[{"role":"user","content":"Say hi in five words."}]}'
```

From another device on the same network, use your Mac’s LAN IP (today often Ethernet `en0`):

```bash
ipconfig getifaddr en0   # or en1 for Wi‑Fi
```

```bash
curl http://192.168.x.x:4000/v1/chat/completions \
  -H "Authorization: Bearer $LOCAL_LLM_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"tiny","messages":[{"role":"user","content":"Say hi in five words."}]}'
```

OpenAI SDKs: set `base_url` / `baseURL` to `http://<mac-ip>:4000/v1` and `api_key` to your `LOCAL_LLM_API_KEY`.

## Commands

| Command | Purpose |
|---------|---------|
| `make setup` / `./scripts/setup.sh` | Bootstrap deps, key, tiny model, binary |
| `make build` | Build `bin/local-llm` |
| `make run` | Serve gateway |
| `make status` | Probe `/health` |
| `make install-launchd` | Start at login (KeepAlive) |
| `make uninstall-launchd` | Remove login agent |
| `./bin/local-llm version` | Print version |

## Add another model config

1. Pull weights: `ollama pull qwen3.5:9b`
2. Edit `config/models.yaml`:

```yaml
models:
  tiny:
    upstream: "smollm2:135m"
  chat:
    upstream: "qwen3.5:9b"
```

3. Restart the gateway.
4. Call `"model": "chat"`.

See `config/models.example.yaml` and `modelfiles/README.md` for more patterns.

### Rough fit on this M4 16 GB Mini

| Difficulty | Examples |
|------------|----------|
| Easy | `smollm2:135m`, `llama3.2:3b`, `qwen2.5:7b`, `llama3.1:8b`, `qwen3.5:9b`, `qwen2.5-coder:7b` |
| Medium | `gemma4:12b`, some 14B Q4 (short context) |
| Skip | Dense 27B+, large MoE files, 70B |

Prefer one mid-size model loaded at a time. Disk (~47 GB free) limits how many you keep.

## Security

- **Ollama stays on `127.0.0.1:11434`.** Do not set `OLLAMA_HOST=0.0.0.0`.
- **Gateway requires** `Authorization: Bearer <LOCAL_LLM_API_KEY>`.
- **Do not port-forward** 4000 or 11434 to the public internet.
- Keep `.env` out of git (already gitignored).
- **macOS Application Firewall:** if other devices cannot connect (or curling your LAN IP returns an empty reply), run once: `make allow-firewall` (admin password). Approve Incoming Connections if macOS prompts.
- Optional: reserve a DHCP lease for this Mac so the LAN IP stays stable.

## Layout

```
cmd/local-llm/     CLI (serve, status, version)
internal/          config, auth, proxy, server
config/models.yaml Model aliases
scripts/setup.sh   One-shot bootstrap
launchd/           Optional login agent template
```
