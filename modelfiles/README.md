# Ollama Modelfiles

Use Modelfiles to create **named presets** on top of the same weights (system prompt, temperature, context).

Example — terse chat style:

```bash
cat > modelfiles/chat-terse.Modelfile <<'EOF'
FROM qwen3.5:9b
PARAMETER temperature 0.3
SYSTEM You are concise. Prefer short answers. Avoid filler.
EOF

ollama create chat-terse -f modelfiles/chat-terse.Modelfile
```

Then add to `config/models.yaml`:

```yaml
  terse:
    upstream: "chat-terse"
```

Restart the gateway (`make run` or reload launchd). Clients call `"model": "terse"`.
