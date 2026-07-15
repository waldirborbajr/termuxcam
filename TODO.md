# 📋 TODO List - termuxcam

**Versão atual:** 0.1.1  
**Objetivo:** Sistema de câmera resiliente, eficiente e monitorável via Telegram no Termux.

---

## ✅ Concluído

- [x] Captura periódica com `termux-camera-photo`
- [x] Envio via Telegram (`sendPhoto`)
- [x] Detecção de movimento via perceptual hash (dHash)
- [x] Heartbeat (envio periódico mesmo sem movimento)
- [x] Configurável via `termuxcam.conf`
- [x] Persistência de estado (`motion_state.json`)
- [x] Wake-lock para não dormir
- [x] Comando `/status` rico com métricas do sistema
- [x] Comandos `/help`, `/log`, `/restart`
- [x] Reset automático de contadores diários à meia-noite

---

## 🔄 Prioridades Atuais (Próximos passos)

### 1. Estabilidade e Resiliência (Alta prioridade)
- [ ] **Sistema de Retry com fila**  
  → Tentativa automática de reenvio de fotos que falharam no upload antes de capturar nova imagem.
- [ ] **Limpeza automática de disco**  
  → Rotacionar/deletar arquivos antigos quando a pasta `camera_captures` atingir limite (ex: 2GB ou 7 dias).
- [ ] **Hot-reload da configuração**  
  → Recarregar `termuxcam.conf` via SIGHUP sem reiniciar o processo.
- [ ] **Melhor tratamento de erros**  
  → Notificação no Telegram quando houver falhas repetidas.

### 2. Inteligência e Detecção (Média/Alta)
- [ ] Detecção avançada de eventos (pessoa, animal, vidro quebrando, etc) usando modelo leve (TFLite ou Python + YOLO)
- [ ] Detecção de som (microfone) para disparar captura
- [ ] Notificação com foto + descrição do evento (ex: "Pessoa detectada")

### 3. Monitoramento e Observabilidade
- [ ] Dashboard web local (Go + HTML) com status em tempo real
- [ ] Monitoramento de hosts via Tailscale (notificar se algum cair)
- [ ] Logging estruturado + opção de envio de logs para Telegram
- [ ] Métricas exportáveis (Prometheus format)

### 4. Qualidade de Vida e Manutenção
- [ ] Suporte a múltiplos chats/telegram (grupos + usuários)
- [ ] Comando `/photo` para tirar foto manualmente
- [ ] Comando `/config` para ver configurações atuais
- [ ] Versão automática + check de atualização
- [ ] Nix-on-droid + configuração declarativa (SOPS para tokens)
- [ ] Testes unitários e integração básicos

### 5. Ideias Avançadas / Futuras
- [ ] Modo "low power" com intervalos maiores quando sem movimento
- [ ] Gravação de vídeo curto ao detectar movimento
- [ ] Integração com Home Assistant ou MQTT
- [ ] Sensor fusion (acelerômetro + câmera + som)
- [ ] Modo "stealth" (sem LED da câmera)

---

## Notas

- **Prioridade atual:** Focar primeiro em **Retry + Limpeza de disco** para tornar o sistema realmente confiável.
- **Próximo grande passo técnico:** Integração com modelo de visão computacional.
- **Meta de estabilidade:** Conseguir rodar por semanas sem intervenção manual.

---

**Última atualização:** 15 de julho de 2026
