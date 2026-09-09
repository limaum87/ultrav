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
| Mock | `mock` (default) | Desenvolvimento/demonstração — sem KVM, host fictício `kvm01` |
| Real | `libvirt` | Produção — exige daemon libvirt acessível |

**Regra de ouro:** se você não tem certeza se o host tem KVM funcional, instale primeiro em modo mock, valide tudo, e só então troque para `libvirt`.

## Fase 0 — Verificação do ambiente

Colete e valide antes de qualquer instalação:

```bash
cat /etc/os-release            # alvo suportado: Ubuntu 22.04+ ou Debian 12+
uname -m                       # x86_64 esperado
id                             # precisa conseguir usar sudo (ou ser root)
ls -la /dev/kvm                # KVM presente? (pode não existir em VM/cloud sem nested virt)
egrep -c '(vmx|svm)' /proc/cpuinfo   # >0 = virtualização habilitada na BIOS
```

- Se `/dev/kvm` não existe ou `egrep` retorna 0: **não é possível rodar VMs reais** neste host. Instale em modo mock e reporte a limitação. Não tente "consertar" a BIOS.
- Porta padrão da aplicação: **8275** (externa). Verifique se está livre: `ss -ltnp | grep 8275`.

## Fase 1 — Instalar Docker (se não existir)

```bash
docker --version || curl -fsSL https://get.docker.com | sh
docker compose version         # precisa ser v2.x
sudo usermod -aG docker "$USER"   # re-login necessário para efeito
```

Validação: `docker run --rm hello-world` deve funcionar sem sudo (ou use sudo em todos os comandos seguintes).

## Fase 2 — Subir o UltraV em modo mock

```bash
cd <diretório-do-repo>
docker compose -f deploy/docker-compose.yml up -d --build
```

O build demora na primeira vez (compila Go + npm build). Depois valide **na ordem**:

```bash
# 1. API viva e pronta (ambos devem retornar {"status":...})
curl -s http://localhost:8275/api/v1/health
curl -s http://localhost:8275/api/v1/ready

# 2. Host mock e 4 VMs fictícias
curl -s http://localhost:8275/api/v1/host | grep -o kvm01
curl -s http://localhost:8275/api/v1/vms | grep -o '"total":[0-9]*'   # esperado: "total":4

# 3. UI e Swagger respondem 200
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8275/
curl -s -o /dev/null -w '%{http_code}\n' http://localhost:8275/docs

# 4. Power action funciona (start + stop da monitoring01)
curl -s -X POST http://localhost:8275/api/v1/vms/monitoring01/start | grep -o '"state":"running"'
curl -s -X POST http://localhost:8275/api/v1/vms/monitoring01/stop | grep -o '"state":"stopped"'
```

Fase 2 completa quando os 4 checks passam. Se o build falhar: rode `docker compose -f deploy/docker-compose.yml logs` e verifique espaço em disco (`df -h`) e conexão com registries.

## Fase 3 — Habilitar KVM/libvirt no host (modo real)

Execute **apenas se** a Fase 0 confirmou virtualização disponível.

```bash
sudo apt-get update
sudo apt-get install -y libvirt-daemon-system libvirt-clients qemu-kvm virtinst
sudo systemctl enable --now libvirtd

# validar
sudo systemctl is-active libvirtd                     # esperado: active
sudo virsh uri                                        # esperado: qemu:///system
sudo virsh list --all                                 # deve funcionar sem erro
ls -l /dev/kvm                                        # deve existir agora
```

Decisão de acesso ao socket do libvirt:

- **Binário nativo no host** (recomendado em produção): crie um usuário dedicado e adicione ao grupo `libvirt`:
  ```bash
  sudo useradd -r -s /usr/sbin/nologin ultrav
  sudo usermod -aG libvirt ultrav
  ```
- **Container**: monte o socket `-v /var/run/libvirt:/var/run/libvirt` (não precisa do grupo).

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

Opção B — container do compose, apontando para o libvirt do host:

```bash
cd deploy
# edite docker-compose.yml do serviço backend:
#   volumes:
#     - /var/run/libvirt:/var/run/libvirt
#   environment:
#     HYPERVISOR_PROVIDER: libvirt
docker compose up -d --build
```

O frontend continua sendo servido pelo serviço `frontend` (porta 8275) e faz proxy do `/api` para o backend — se o backend rodar nativo na porta 8080 do host, ajuste o `proxy_pass` em `frontend/nginx.conf` para `http://host.docker.internal:8080` (Linux: adicione `extra_hosts: ["host.docker.internal:host-gateway"]`).

Validação do modo real:

```bash
curl -s http://localhost:8275/api/v1/ready
# {"status":"ready","hypervisor":"ready"} = libvirt OK
# {"status":"not-ready",...} ou 503 = problema de conexão; veja Fase 6

curl -s http://localhost:8275/api/v1/host | python3 -m json.tool | head -20
# hostname/kernel/versões devem refletir o host REAL (não "kvm01")

curl -s http://localhost:8275/api/v1/storage/pools   # pools reais (default, etc.)
curl -s http://localhost:8275/api/v1/networks        # rede default do libvirt
```

Crie uma VM de teste para validar power actions de ponta a ponta:

```bash
# cria uma VM descartável 128 MB via virt-install (TCG funciona mesmo sem /dev/kvm,
# mas será lenta; com KVM é rápida)
sudo virt-install --name ultrav-test --memory 128 --vcpus 1 \
  --disk none --network network=default --noautoconsole \
  --import --boot hd || true
# alternativa se não tiver imagem: apenas valide listagem de domínios shutoff:
sudo virsh list --all

curl -s http://localhost:8275/api/v1/vms | grep ultrav-test
```

## Fase 5 — Acesso externo e firewall

- UI/API: porta **8275/TCP**. Libere no firewall se necessário: `sudo ufw allow 8275/tcp`.
- O acesso ao libvirt fica **local** ao host (socket unix); nunca exponha a porta 16509 do libvirt.
- A fase 1 do produto não tem autenticação: **não exponha a porta 8275 publicamente** sem colocar um proxy com auth na frente. Prefira VPN/rede de gerência.

## Fase 6 — Troubleshooting

| Sintoma | Causa provável | Correção |
|---|---|---|
| `/api/v1/ready` retorna 503 com `hypervisor: unavailable` | daemon libvirt parado ou socket inacessível | `sudo systemctl start libvirtd`; confira permissão de grupo no socket `ls -l /var/run/libvirt/libvirt-sock` |
| `cannot connect to libvirt (qemu:///system)` nos logs | container sem o socket montado | monte `-v /var/run/libvirt:/var/run/libvirt` no serviço backend |
| VMs listadas mas sem `ipAddress` | QEMU Guest Agent não instalado no guest | instale `qemu-guest-agent` dentro da VM (normal: IP fica `null`) |
| `startVirtualMachine` retorna 500 em host sem `/dev/kvm` | domínio exige KVM e o host não tem | use modo mock ou habilite nested virtualization na BIOS do hipervisor físico |
| Porta 8275 ocupada | outro serviço | mude a porta publicada no `deploy/docker-compose.yml` (`"8275:80"`) |
| UI abre mas sem dados | backend caiu ou proxy errado | `docker compose -f deploy/docker-compose.yml ps` + `logs backend` |

Logs estruturados do backend: `docker compose -f deploy/docker-compose.yml logs -f backend` (JSON por linha, inclui `requestId`).

## Fase 7 — Validação final (checklist do agente)

Marque somente se **todos** passarem:

- [ ] `GET /api/v1/health` → 200 `{"status":"ok"}`
- [ ] `GET /api/v1/ready` → 200 com `hypervisor: ready`
- [ ] `GET /openapi.json` → 200 e `GET /docs` → 200 (Swagger mostra 17 endpoints)
- [ ] UI em `http://<host>:8275` carrega Dashboard, Virtual Machines, Storage, Network
- [ ] No modo real: host mostrado é o hostname real; pools/networks reais aparecem
- [ ] Pelo menos uma power action executada com sucesso via API (e o estado reflete no `virsh list`)
- [ ] Nada além da porta 8275 exposto

## Referência rápida de configuração (env vars)

| Variável | Default | Descrição |
|---|---|---|
| `HYPERVISOR_PROVIDER` | `mock` | `mock` ou `libvirt` |
| `HYPERVISOR_LIBVIRT_URI` | `qemu:///system` | URI de conexão do libvirt (`qemu+ssh://host/system` também funciona) |
| `ULTRAV_PORT` | `8080` | Porta interna do backend (externa no compose: 8275 via nginx) |
| `ULTRAV_CORS_ORIGIN` | *(vazio)* | Origin do frontend quando acessado fora do proxy (compose não precisa) |

## O que o agente NÃO deve fazer

- Não instalar Kubernetes, microserviços ou componentes fora da stack descrita.
- Não modificar `docs/api/openapi.yaml` para "fazer a UI funcionar" — o contrato é a fonte da verdade; se faltar algo, gere tipos com `./scripts/generate.sh` após alterar o contrato.
- Não expor shell/execução de comandos na API nem contornar o `HypervisorProvider` (nenhum handler pode importar libvirt).
- Não habilitar backup, multi-host, MCP ou IA — estão fora de escopo até as fases 3–5 do roadmap (`ROADMAP.md`).
