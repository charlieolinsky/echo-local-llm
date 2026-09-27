# Ollama Modelfiles

Use Modelfiles to create **named presets** on top of the same weights (system prompt, temperature, context).

## Included: `summarize`

```bash
ollama create summarize -f modelfiles/summarize.Modelfile
```

Wired in `config/models.yaml` as alias `summarize` (same Qwen 3.5 9B weights, summarization system prompt).

## Custom preset example

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

Restart the gateway. Clients call `"model": "terse"`.
