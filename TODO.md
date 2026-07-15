# Melhorias no termuxcam

## Detecção de mudança / movimento (reduz ruído e custo)

Comparar a foto atual com a anterior (diff de hash perceptual ou simples diferença de bytes) e só enviar pro Telegram quando houver mudança significativa. Isso resolve o problema de "288 fotos por dia" que discutimos — a maioria idêntica ao ambiente parado.
Retry com backoff em vez de só "keep local file"
Hoje, se o upload falhar, o arquivo fica local até o próximo ciclo tentar de novo do zero (ciclo novo, foto nova). Um retry queue simples — tentar reenviar arquivos antigos não enviados antes de capturar um novo — evitaria perder momentos específicos por falha de rede momentânea.

## Rotação/limite de espaço em disco

Se o upload falhar repetidamente (Wi-Fi caiu por horas), camera_captures/ pode crescer indefinidamente. Um limite de tamanho total ou idade máxima do arquivo evita encher o armazenamento do celular.

## Métricas/heartbeat

Um comando /status no bot do Telegram que responde com uptime, última captura bem-sucedida, e espaço em disco — útil pra você verificar remotamente se está tudo funcionando sem precisar abrir o Termux fisicamente.

## Config com hot-reload

Hoje precisa de sv restart pra aplicar mudanças no .conf. Um SIGHUP handler que recarrega a config sem matar o processo (e sem perder o wake-lock) seria um upgrade natural, já que você já tem o signal.NotifyContext no lugar.

## A peça de visão computacional que desenhamos antes

Voltando à arquitetura Go (captura) + Fedora (Python/YOLO) que montamos — isso continua sendo o próximo passo mais rico pro seu objetivo de estudo em CV/ML, e se encaixa bem como uma extensão modular do que já existe.
Outras ideias pro Termux, dado seu perfil

## Espelhar parte do seu workflow NixOS

Você já gerencia dotfiles declarativamente nos três hosts — dá pra ter uma versão leve disso no Termux com Nix puro (pkg install nix) ou nix-on-droid, aplicando os mesmos princípios (SOPS pros segredos do bot do Telegram, por exemplo, em vez de token em texto puro no run script).
Monitoramento da sua rede via Tailscale
Um dashboard simples (Go + HTML servido localmente) que faz ping/healthcheck nos seus outros hosts da tailnet (dell1456, macutm, mac2011) e notifica no Telegram se algum cair — reaproveitando a mesma infra de bot que você já tem funcionando.

## Sensor logging

termux-sensor dá acesso a acelerômetro, giroscópio, luz, proximidade — um projeto de "detector de queda" ou "log de exposição à luz ao longo do dia" é um exercício interessante de streaming de dados + análise simples, sem precisar de hardware extra.

## SSH bastion móvel

Com Tailscale já rodando no celular, ele pode servir como salto de emergência pra acessar seus hosts NixOS quando estiver fora de casa e o notebook não estiver à mão — vale configurar sshd no Termux (pkg install openssh) com chave pública, não senha.
Qual dessas direções te atrai mais agora — continuar refinando o termuxcam (detecção de movimento seria o ganho mais imediato), ou partir pra algo novo tipo o dashboard de monitoramento da tailnet?
