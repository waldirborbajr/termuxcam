# 📋 TODO List - termuxcam

**Versão atual:** 0.1.1  
**Objetivo:** Sistema de câmera resiliente, eficiente e monitorável via Telegram no Termux.

---

## ✅ Concluído

- [x] Captura periódica com `termux-camera-photo`
- [x] Envio via Telegram
- [x] Detecção de movimento via perceptual hash
- [x] Heartbeat
- [x] Configurável via arquivo
- [x] Comando `/status` rico
- [x] Comandos `/help`, `/log`, `/restart`
- [x] Reset automático de contadores diários

---

## 🔄 Em Progresso / Alta Prioridade

### Novas Funcionalidades Solicitadas

- [ ] **Comando `/photo`**  
  → Tirar foto manualmente sob demanda via Telegram.

- [ ] **Comando `/config`**  
  → Exibir configurações atuais carregadas (interval, camera mode, motion, etc).

- [ ] **Hot-reload da configuração**  
  → Recarregar `termuxcam.conf` via sinal SIGHUP sem precisar reiniciar o processo (mantendo wake-lock e estado).

---

## 📌 Próximas Prioridades (em ordem sugerida)

### Estabilidade
- [ ] Sistema de Retry com fila para uploads falhados
- [ ] Rotação / Limpeza automática de arquivos antigos (limite de disco)
- [ ] Melhor tratamento e notificação de erros persistentes

### Inteligência
- [ ] Detecção avançada (modelo leve de visão computacional)
- [ ] Detecção de som para disparar captura

### Monitoramento
- [ ] Dashboard web local simples
- [ ] Monitoramento de hosts via Tailscale
- [ ] Logging estruturado + envio de logs

### Qualidade de Vida
- [ ] Versão automática + check de update no status
- [ ] Nix-on-droid + configuração declarativa
- [ ] Modo low-power quando sem movimento

---

**Última atualização:** 15 de julho de 2026
