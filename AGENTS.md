# AGENTS.md — Guia de instalação do UltraV

Este documento orienta um **agente autônomo** (ou humano) a instalar e validar o UltraV em um host Linux, do zero, sem decisões ambíguas. Siga as fases em ordem e **valide cada fase antes de avançar**.

## Visão geral do produto

UltraV é uma camada de gerenciamento API-first para VMs KVM/QEMU. Componentes:

- **Backend** (Go): REST API em `/api/v1`, binário único `ultrav`
- **Frontend** (React/Vite): SPA servida com proxy para a API (nginx)
- **Hipervisor**: libvirt + QEMU/KVM (opcional no modo mock)

Dois modos de operação:

| Modo | `HYPERVISOR_PROVIDER` | Quando usar |
|------|----------------------|-------------|
| Mock | `mock` (default do binário) | Desenvolvimento/demonstração — sem KVM, host fictício `kvm01` |
| Real | `libvirt` (default do compose) | Produção — exige daemon libvirt acessível |

**Regra de ouro:** se você não tem certeza se o host tem KVM funcional, instale primeiro em modo mock, valide tudo, e só então troque para `libvirt`.

## Fase 0 — Verificação do ambiente

Colete e valide antes de qualquer instalação:

```bash
cat /etc/os-release            # alvo suportado: Ubuntu 22.04+ ou Debian 12+
uname -m                       # x86_64 esperado
id                             # precisa conseguir usar sudo (ou ser root)
ls -la /dev/kvm                # KVM presente? (pode não existir em VM/cloud sem nested virt)
egrep -c '(vmx|svm)' /proc/cpuinfo   # >0 = virtualização habilitada na BIOS
getent group libvirt kvm       # GIDs do host -> LIBVIRT_GID / KVM_GID no deploy/.env
```

> **Nunca hardcode GIDs.** Eles mudam por distro e por instalação: Debian 12 usa
> `137`/`104`; Ubuntu 26.04 usa `982`/`992`. Com o GID errado o container sobe,
> fica `healthy`, e só falha ao falar com o libvirt ou ao gravar nos diretórios.

- Se `/dev/kvm` não existe ou `egrep` retorna 0: **não é possível rodar VMs reais** neste host. Instale em modo mock e reporte a limitação. Não tente "consertar" a BIOS.
- Porta padrão da aplicação: **8275** (externa). Verifique se está livre: `ss -ltnp | grep 8275`.

## Fase 1 — Instalar Docker (se não existir)

```bash
docker --version || curl -fsSL https://get.docker.com | sh
docker compose version         # precisa ser v2.x
sudo usermod -aG docker "$USER"   # re-login necessário para efeito
```

Em releases muito recentes (ex.: Ubuntu 26.04 "resolute") o script
`get.docker.com` pode não reconhecer a distribuição. Nesse caso use os pacotes
da distro, que já trazem o Compose v2:

```bash
sudo apt-get install -y docker.io docker-compose-v2
```

Validação: `docker run --rm hello-world` deve funcionar sem sudo (ou use sudo em todos os comandos seguintes).

## Fase 2 — Criar o `deploy/.env` e subir a stack

O compose não embute valores do host: GIDs, IP do certificado e segredo JWT vêm
de `deploy/.env` (o Compose carrega esse arquivo automaticamente quando o `-f`
aponta para `deploy/`).

```bash
cd <diretório-do-repo>
cp deploy/.env.example deploy/.env

getent group libvirt | cut -d: -f3   # -> LIBVIRT_GID
getent group kvm     | cut -d: -f3   # -> KVM_GID
openssl rand -hex 32                 # -> ULTRAV_JWT_SECRET
$EDITOR deploy/.env                  # ajuste ULTRAV_HOST_IP e ULTRAV_ADMIN_PASSWORD

docker compose -f deploy/docker-compose.yml up -d --build
```

Variável obrigatória ausente aborta o Compose com mensagem explícita
(`defina LIBVIRT_GID em deploy/.env`) — melhor do que subir com o GID errado e
só quebrar no acesso ao socket.

O build demora na primeira vez (compila Go + npm build). A UI é **HTTPS**
(noVNC exige contexto seguro), com certificado autoassinado gerado no build —
use `-k` no curl:

```bash
B=https://localhost:8275
curl -sk $B/api/v1/health                            # {"status":"ok"}
curl -sk $B/api/v1/ready                             # {"status":"ready","hypervisor":"ready"}
curl -sk -o /dev/null -w '%{http_code}\n' $B/        # 200
curl -sk -o /dev/null -w '%{http_code}\n' $B/docs    # 200
```

Todos os endpoints, exceto `health` e `ready`, exigem login (JWT):

```bash
TOKEN=$(curl -sk -X POST $B/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin@2026"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
curl -sk $B/api/v1/host -H "Authorization: Bearer $TOKEN"
```

**Modo mock (sem KVM).** Com `HYPERVISOR_PROVIDER=mock` no `deploy/.env` o host é
o fictício `kvm01` com 4 VMs; os bind mounts do libvirt ficam inertes:

```bash
curl -sk $B/api/v1/host -H "Authorization: Bearer $TOKEN" | grep -o kvm01
curl -sk $B/api/v1/vms  -H "Authorization: Bearer $TOKEN" | grep -o '"total":[0-9]*'   # "total":4
curl -sk -X POST $B/api/v1/vms/monitoring01/start -H "Authorization: Bearer $TOKEN" | grep -o '"state":"running"'
curl -sk -X POST $B/api/v1/vms/monitoring01/stop  -H "Authorization: Bearer $TOKEN" | grep -o '"state":"stopped"'
```

Se o build falhar: `docker compose -f deploy/docker-compose.yml logs` e confira
espaço em disco (`df -h`) e acesso aos registries.

## Fase 3 — Habilitar KVM/libvirt no host (modo real)

Execute **apenas se** a Fase 0 confirmou virtualização disponível.

```bash
sudo apt-get update
sudo apt-get install -y qemu-system-x86 qemu-utils libvirt-daemon-system \
  libvirt-clients libvirt-daemon-driver-qemu virtinst bridge-utils ovmf dnsmasq-base
sudo systemctl enable --now libvirtd virtlogd

# validar
sudo systemctl is-active libvirtd                     # esperado: active
sudo virsh uri                                        # esperado: qemu:///system
sudo virsh list --all                                 # deve funcionar sem erro
ls -l /dev/kvm                                        # deve existir agora
```

> **O pacote `qemu-kvm` não existe no Ubuntu 26.04.** Era um meta-pacote nas
> releases antigas; o nome atual (válido em 22.04/24.04/26.04) é
> `qemu-system-x86`. Se o `apt` responder "Candidato: (nenhum)", é esse o motivo.

**Pool de discos `default` (passo obrigatório).** O libvirt recente **não cria**
o pool `default`; sem ele a UI mostra 0 pools e `POST /vms` falha, porque
`disk.poolId` é obrigatório. Crie uma vez:

```bash
sudo virsh pool-define-as default dir --target /var/lib/libvirt/images
sudo virsh pool-build default
sudo virsh pool-start default
sudo virsh pool-autostart default
sudo virsh pool-list --all                            # default  active  yes
```

Decisão de acesso ao socket do libvirt:

- **Binário nativo no host** (recomendado em produção): crie um usuário dedicado e adicione ao grupo `libvirt`:
  ```bash
  sudo useradd -r -s /usr/sbin/nologin ultrav
  sudo usermod -aG libvirt ultrav
  ```
- **Container**: monte o socket `-v /var/run/libvirt:/var/run/libvirt` (o compose já faz) e informe o GID do grupo `libvirt` em `LIBVIRT_GID` no `deploy/.env`.

## Fase 4 — Rodar o UltraV em modo libvirt

Opção A — binário nativo (build fora do host de destino ou com Go 1.23+ instalado):

```bash
# build (requer Go 1.23+; o build usa a tag libvirt_dlopen, não precisa de libvirt-dev)
cd backend
CGO_ENABLED=1 go build -tags libvirt_dlopen -o ultrav ./cmd/ultrav

# rodar (como usuário do grupo libvirt, ou root)
HYPERVISOR_PROVIDER=libvirt \
HYPERVISOR_LIBVIRT_URI=qemu:///system \
ULTRAV_PORT=8080 ./ultrav
```

Opção B — container do compose (recomendado). A stack já vem em modo libvirt e
lê os parâmetros de `deploy/.env` (Fase 2); o que importa para o modo real:

```yaml
# deploy/.env
LIBVIRT_GID=982              # getent group libvirt | cut -d: -f3
KVM_GID=992                  # getent group kvm     | cut -d: -f3
ULTRAV_HOST_IP=192.168.8.63  # IP pelo qual a UI é acessada (SAN do certificado)
```

```bash
docker compose -f deploy/docker-compose.yml up -d --build
```

**Regra dos caminhos (backend em container + libvirt real).** O backend grava
caminhos absolutos no XML do domínio (ISOs, volumes de disco), mas quem abre
esses arquivos é o **qemu, no host**. Todo diretório que alimenta o XML precisa
ser um bind mount de **caminho idêntico** dentro e fora do container — nunca um
volume nomeado. Com volume nomeado o upload funciona, a validação de existência
(feita dentro do container) passa, e a VM só falha no start com
`Cannot access storage file`.

Vale para **todos** os diretórios montados: ISOs, imagens de disco (restore) e a
biblioteca de backups. O usuário do container (`nobody`) precisa escrever neles;
ele entra no grupo `kvm` via `group_add` (`KVM_GID`), então permissão de grupo com
escrita basta:

```bash
sudo mkdir -p /var/lib/libvirt/isos /var/lib/ultrav/backups
sudo chgrp kvm /var/lib/libvirt/isos /var/lib/libvirt/images /var/lib/ultrav/backups
sudo chmod 2775 /var/lib/libvirt/isos /var/lib/libvirt/images /var/lib/ultrav/backups

# teste rápido: como o container enxerga os diretórios (usa o container do compose)
docker exec deploy-backend-1 sh -c \
  'id; touch /var/lib/libvirt/isos/.probe && rm /var/lib/libvirt/isos/.probe && echo OK'
```

O backend também monta o `/etc/os-release` do host: sem isso o Dashboard mostra o
SO do container (`Debian GNU/Linux 12`) em vez do SO real.

O frontend continua sendo servido pelo serviço `frontend` (porta 8275, **HTTPS**)
e faz proxy do `/api` para o backend — se o backend rodar nativo na porta 8080 do
host, ajuste o `proxy_pass` em `frontend/nginx.conf` para
`http://host.docker.internal:8080` (Linux: adicione
`extra_hosts: ["host.docker.internal:host-gateway"]`).

Validação do modo real:

```bash
B=https://localhost:8275
TOKEN=$(curl -sk -X POST $B/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin@2026"}' \
  | python3 -c 'import sys,json;print(json.load(sys.stdin)["token"])')
AUTH="Authorization: Bearer $TOKEN"

curl -sk $B/api/v1/ready -H "$AUTH"
# {"status":"ready","hypervisor":"ready"} = libvirt OK
# {"status":"not-ready",...} ou 503 = problema de conexão; veja Fase 6

curl -sk $B/api/v1/host -H "$AUTH" | python3 -m json.tool | head -20
# hostname/kernel/SO devem refletir o host REAL (não "kvm01")

curl -sk $B/api/v1/capabilities -H "$AUTH"   # virtualization.kvm deve ser true
curl -sk $B/api/v1/storage/pools -H "$AUTH"  # pool default
curl -sk $B/api/v1/networks -H "$AUTH"       # rede default do libvirt
```

Crie uma VM de teste para validar power actions de ponta a ponta. O caminho mais
fiel é pela própria API (aloca disco no pool e define o domínio):

```bash
curl -sk -X POST $B/api/v1/vms -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"name":"ultrav-test","vcpus":1,"memoryBytes":536870912,
       "disk":{"poolId":"default","sizeBytes":2147483648,"format":"qcow2"},
       "networkId":"default","start":false}'
# 202 + task: acompanhe em GET /api/v1/tasks/{id} até "succeeded"

curl -sk -X POST $B/api/v1/vms/ultrav-test/start -H "$AUTH"   # state: running
sudo virsh list                                               # ultrav-test running
curl -sk -X POST $B/api/v1/vms/ultrav-test/stop  -H "$AUTH"   # state: stopped
curl -sk -X DELETE $B/api/v1/vms/ultrav-test -H "$AUTH"       # limpa o teste
```

## Fase 5 — Acesso externo e firewall

- UI/API: porta **8275/TCP**, servida em **HTTPS** (certificado autoassinado gerado
  no build; o browser avisa uma vez). O SAN vem de `ULTRAV_HOST_IP` — abra a UI
  pelo endereço configurado, senão o browser acusa nome divergente.
- **A porta é aberta por design.** O UltraV é pensado para acesso direto na rede
  de gerência/LAN, sem regra de firewall: o controle de acesso é o login JWT, não
  o `ufw`/`iptables`. Porta aberta aqui **não é pendência de instalação** — é o
  modo de operação esperado.
- O que continua proibido: expor a 8275 na internet e expor a 16509 do libvirt.
  Para acesso fora da LAN, use VPN ou um proxy com autenticação na frente.
- O acesso ao libvirt é **local** ao host (socket unix), nunca pela rede.
- Login inicial: `admin` + `ULTRAV_ADMIN_PASSWORD` do `deploy/.env` (default
  `admin@2026`), criado apenas no primeiro boot com banco vazio. Troque a senha
  depois em `PUT /api/v1/auth/password` (mudar a variável não altera um admin já
  existente) e mantenha um `ULTRAV_JWT_SECRET` próprio.

## Fase 6 — Troubleshooting

| Sintoma | Causa provável | Correção |
|---|---|---|
| `/api/v1/ready` retorna 503 com `hypervisor: unavailable` | daemon libvirt parado ou socket inacessível | `sudo systemctl start libvirtd`; confira `ls -l /var/run/libvirt/libvirt-sock` |
| `cannot connect to libvirt (qemu:///system)` nos logs | container sem o socket montado **ou** `LIBVIRT_GID` errado | confira o bind mount, rode `getent group libvirt`, corrija o `deploy/.env` e `up -d` |
| Container `healthy` mas o libvirt não responde | `KVM_GID`/`LIBVIRT_GID` errados: o container sobe e só falha no acesso | `getent group kvm libvirt`, ajuste o `.env`, `docker compose up -d` |
| `apt`: `qemu-kvm ... Candidato: (nenhum)` | pacote removido em releases novas (26.04) | instale `qemu-system-x86` (Fase 3) |
| UI mostra 0 pools / `POST /vms` retorna 400 | pool `default` não existe no libvirt | crie o pool (Fase 3, "Pool de discos default") |
| Browser: certificado inválido ou nome divergente | `ULTRAV_HOST_IP` diferente do endereço usado | ajuste no `.env` e `up -d --build` (o certificado é gerado no build) |
| Sessões caem a cada restart do backend | `ULTRAV_JWT_SECRET` ausente ou efêmero | preencha no `deploy/.env` (`openssl rand -hex 32`) |
| Dashboard mostra "Debian GNU/Linux 12" | `/etc/os-release` do host não montado | o compose monta; se removeu, volte a montar |
| VMs listadas mas sem `ipAddress` | QEMU Guest Agent não instalado no guest | instale `qemu-guest-agent` dentro da VM (normal: IP fica `null`) |
| `Cannot access storage file '<dir>/<arquivo>.iso'` ao ligar a VM | diretório em volume nomeado: o caminho do XML não existe no host | bind mount de caminho idêntico + `ULTRAV_ISO_DIR` (Fase 4) |
| `startVirtualMachine` retorna 500 em host sem `/dev/kvm` | domínio exige KVM e o host não tem | use modo mock ou habilite nested virtualization no hipervisor físico |
| Porta 8275 ocupada | outro serviço | mude a porta publicada no `deploy/docker-compose.yml` (`"8275:443"`) |
| UI abre mas sem dados | backend caiu ou proxy errado | `docker compose -f deploy/docker-compose.yml ps` + `logs backend` |

Logs estruturados do backend: `docker compose -f deploy/docker-compose.yml logs -f backend` (JSON por linha, inclui `requestId`).

## Fase 7 — Validação final (checklist do agente)

Marque somente se **todos** passarem:

- [ ] `GET /api/v1/health` → 200 `{"status":"ok"}`
- [ ] `GET /api/v1/ready` → 200 com `hypervisor: ready`
- [ ] `GET /openapi.json` → 200 e `GET /docs` → 200 (o contrato atual tem 49 operações)
- [ ] `POST /api/v1/auth/login` com `admin` → 200 e token JWT
- [ ] UI em `https://<ULTRAV_HOST_IP>:8275` carrega Dashboard, Virtual Machines, Storage, Network
- [ ] Modo real: hostname/SO reais, `capabilities.virtualization.kvm == true`, pool `default` e rede `default` visíveis
- [ ] Pelo menos uma power action executada com sucesso via API (e o estado reflete no `virsh list`)
- [ ] Nada além da porta 8275 exposto (o firewall **não** é usado como controle de acesso — ver Fase 5)

## Referência rápida de configuração (env vars)

| Variável | Default | Descrição |
|---|---|---|
| `HYPERVISOR_PROVIDER` | `mock` no binário, `libvirt` no compose | `mock` ou `libvirt` |
| `HYPERVISOR_LIBVIRT_URI` | `qemu:///system` | URI de conexão do libvirt (`qemu+ssh://host/system` também funciona) |
| `ULTRAV_PORT` | `8080` | Porta interna do backend (externa no compose: 8275 via nginx) |
| `ULTRAV_CORS_ORIGIN` | *(vazio)* | Origin do frontend quando acessado fora do proxy (compose não precisa) |
| `ULTRAV_ISO_DIR` | `/var/lib/libvirt/images/isos` | Biblioteca de ISOs (upload pela UI, anexada como mídia no wizard). Em container + libvirt **precisa** ser bind mount de caminho idêntico ao do host (ver "Regra dos caminhos", Fase 4). Pode mudar em runtime: ao criar um storage pool com `isoLibrary: true`, o `targetPath` do pool passa a ser a biblioteca (persistido em `ULTRAV_SETTINGS_PATH`) |
| `ULTRAV_SETTINGS_PATH` | `<dir do ULTRAV_DB_PATH>/settings.json` | Settings persistidas (hoje só o override do diretório da ISO library) |
| `ULTRAV_JWT_SECRET` | *(obrigatório no compose)* | Segredo das sessões. Sem ele o binário gera um efêmero (warning no log) e as sessões caem a cada restart. Gere com `openssl rand -hex 32` |
| `ULTRAV_ADMIN_USER` | `admin` | Usuário criado no primeiro boot (banco vazio) |
| `ULTRAV_ADMIN_PASSWORD` | `admin@2026` (compose) | Senha do admin inicial; se vazia, o backend gera uma e registra no log |
| `LIBVIRT_GID` / `KVM_GID` | *(obrigatório no compose)* | GIDs do host para o `group_add` do container (`getent group libvirt kvm`) |
| `ULTRAV_HOST_IP` | *(obrigatório no compose)* | IP/DNS do host que entra no SAN do certificado TLS gerado no build |

## O que o agente NÃO deve fazer

- Não instalar Kubernetes, microserviços ou componentes fora da stack descrita.
- Não modificar `docs/api/openapi.yaml` para "fazer a UI funcionar" — o contrato é a fonte da verdade; se faltar algo, gere tipos com `./scripts/generate.sh` após alterar o contrato.
- Não expor shell/execução de comandos na API nem contornar o `HypervisorProvider` (nenhum handler pode importar libvirt).
- Não habilitar backup, multi-host, MCP ou IA — estão fora de escopo até as fases 3–5 do roadmap (`ROADMAP.md`).
