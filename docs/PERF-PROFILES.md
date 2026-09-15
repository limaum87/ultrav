# Plano — Perfis de performance por SO (Linux / Windows / Other)

> Feature: perfis de performance aplicados na criação de VM, com Hyper-V enlightenments
> para Windows e detecção de capacidades do host. Primeiro caso de uso real: migração
> de VMs Windows (TS/aplicação) e Linux (Samba AD/fileserver) saindo do Hyper-V.

## Problema

Hoje `CreateVirtualMachine` (`backend/internal/hypervisor/libvirt/vms.go`) monta o
XML de domínio com `fmt.Sprintf` genérico:

- disco virtio-blk sem `cache`/`io`/`iothread`
- `<features><acpi/><apic/></features>` apenas
- `<clock offset='utc'/>` simples
- sem `<cpu>`, sem canal do QEMU Guest Agent

Consequências:

1. Windows muito mais lento que no Hyper-V (sem enlightenments, relógio instável).
2. Instalador do Windows não vê o disco virtio (falta driver → precisa ISO virtio-win).

## Princípios do repo respeitados

- **Contract-first**: `docs/api/openapi.yaml` é a fonte da verdade; regenerar com
  `./scripts/generate.sh` após alterar.
- **`HypervisorProvider` é a única fronteira** com libvirt; nenhum handler importa libvirt.
- Mock atualizado em paralelo (UI/testes sem KVM).
- Operações semanticamente específicas, sem shell/exec.

---

## 1. Contrato (`docs/api/openapi.yaml`)

Em `VirtualMachineCreate`:

- [ ] `osType`: enum `linux | windows | other`, default `linux`, opcional.
- [ ] `virtioDriversIsoId`: string opcional (ID de ISO da biblioteca,
  ex. `virtio-win-0.1.x.iso`). Só vale para `windows`.

Em `VirtualMachine` (leitura):

- [ ] `osType`: detectado do XML (presença de `<hyperv>` → `windows`).
- [ ] `performanceProfile`: bloco com o que foi aplicado:
  - `cpuMode` (host-passthrough / host-model / custom)
  - `diskBus` (scsi / sata)
  - `cache` (none)
  - `ioThreads` (0/1)
  - `hypervEnlightenments[]` (lista efetivamente aplicada)
- [ ] `warnings[]` (ou campo equivalente) na **resposta de criação**:
  ex. `windows` sem `virtioDriversIsoId` → aviso de que o instalador não verá o
  disco virtio-scsi sem carregar `vioscsi` (não falhar).
- [ ] Documentar no `description` do endpoint POST /vms o que cada perfil aplica.

## 2. Gerador de XML testável

- [ ] Extrair a montagem do domínio do `fmt.Sprintf` para **função pura**
      `buildDomainXML(spec domainSpec, caps hostFeatures) (string, error)`.
      Preferir structs + `encoding/xml` (ou `text/template` com escape correto).
      Proibido concatenar string de usuário sem escape.
- [ ] Testes unitários table-driven: fazer **parse do XML de volta** e verificar
      elementos (não comparação de string inteira). Cenários:
  - linux (perfil comum)
  - windows com ISO de drivers (2 CD-ROMs, boot order correto)
  - windows sem ISO de drivers (1 CD-ROM)
  - windows com capabilities reduzidas (simulando QEMU ≤ 4.2 — enlightenments
    não suportados ficam de fora e são logados)
  - other (SATA, e1000e, host-model, sem hyperv)

## 3. Perfil comum (linux e windows)

- `<cpu mode='host-passthrough' check='none' migratable='on'/>`
  *(documentar: host-passthrough limita live migration entre CPUs diferentes —
  tema da fase multi-host)*
- `<iothreads>1</iothreads>`; controlador
  `<controller type='scsi' model='virtio-scsi'><driver queues='N' iothread='1'/></controller>`,
  N = vCPUs (máx. 8).
- Disco `bus='scsi'` (sdX) com
  `<driver name='qemu' type='<fmt>' cache='none' io='native' discard='unmap' detect_zeroes='unmap'/>`.
  Fallback para `io='threads'` se `native` der problema no host.
- NIC virtio com `<driver name='vhost' queues='N'/>` (N = vCPUs, máx. 8).
- Canal do guest agent:
  `<channel type='unix'><target type='virtio' name='org.qemu.guest_agent.0'/></channel>`
  (hoje o código de IP depende do agente, mas o canal nunca é criado).
- `<memballoon model='none'/>` por padrão (DC/banco não gostam de balloon).
- Timers: `rtc` catchup, `pit` delay, `hpet` present='no'.
- `<rng model='virtio'><backend model='random'>/dev/urandom</backend></rng>`.

## 4. Perfil windows (além do comum)

- `<clock offset='localtime'>` + `<timer name='hypervclock' present='yes'/>`.
- `<features><hyperv mode='custom'>` com: `relaxed`, `vapic`, `spinlocks`
  (retries=8191), `vpindex`, `synic`, `stimer`, `runtime`, `frequencies`,
  `reset`, `tlbflush`, `ipi`.
- **Detecção de suporte**: NÃO assumir versão — ler `virConnectGetDomainCapabilities`
  (e/ou versões de libvirt/QEMU) e incluir só enlightenments suportados.
  Host de teste existe com QEMU ≤ 4.2. Log estruturado dos que ficaram de fora;
  refletir em `performanceProfile.hypervEnlightenments`.
- **Instalação sem driver**: se `virtioDriversIsoId` preenchido, anexar como
  segundo CD-ROM SATA (ISO de instalação continua 1ª, boot order 1).
  Validar ID com o mesmo `iso.ValidID` já usado.
- Se `windows` sem ISO de drivers: não falhar; devolver `warnings[]`.
- Vídeo: manter VNC por socket unix (console web depende). Só trocar `vga`→`virtio`
  se o noVNC continuar funcionando; senão manter `vga`.

## 5. Perfil other (compatibilidade máxima)

- Disco SATA, NIC `e1000e`, sem hyperv, `cpu mode='host-model'`.
- Uso: sistemas estranhos ou primeira subida de VHDX convertido sem virtio.

## 6. Mock provider

- [ ] Aceitar campos novos (`osType`, `virtioDriversIsoId`) e devolver
      `osType`/`performanceProfile` coerentes (UI e testes de handler sem KVM).

## 7. UI (React/Vite)

- [ ] Wizard de criação: select "Sistema operacional" (Linux / Windows / Outro);
      quando Windows, select da biblioteca de ISOs para "ISO de drivers VirtIO"
      + dica de download (fedorapeople virtio-win stable).
- [ ] Mostrar os `warnings[]` retornados na criação.
- [ ] Detalhes da VM: seção "Perfil de performance" lendo `performanceProfile`.

## 8. Docs

- [ ] `ARCHITECTURE.md`: decisão de perfis por SO + feature detection.
- [ ] `ROADMAP.md`: itens novos (PATCH de perfil em VM existente, etc.).
- [ ] `README.md`: passo a passo curto de instalação de Windows — carregar
      `vioscsi` e `NetKVM` da ISO virtio-win no instalador, depois rodar
      `virtio-win-guest-tools.exe` (inclui o guest agent).

## Fora do escopo (não fazer agora)

- UEFI / Secure Boot / TPM (swtpm) para Windows 11 — mas estruturar o gerador
  para entrar depois sem reescrita.
- Hugepages, pinning de vCPU, NUMA.
- Conversão/importação de VHDX.
- Aplicar perfil em VM existente via PATCH → **registrar no ROADMAP**.

## Critérios de aceite

- [ ] `./scripts/generate.sh` roda e o frontend compila contra o contrato novo.
- [ ] `go test ./...` passa; gerador cobre linux, windows (com/sem ISO de drivers,
      e com capabilities reduzidas simulando QEMU antigo) e other.
- [ ] Em host libvirt real: VM windows criada — `virsh dumpxml` mostra
      `hyperv`, `hypervclock`, `host-passthrough`, virtio-scsi com iothread,
      `cache='none'`, `discard='unmap'`, canal do guest agent e os dois CD-ROMs.
- [ ] VM linux criada sobe e mostra IP via guest agent (com `qemu-guest-agent` no guest).
- [ ] Console web funciona nos três perfis.
- [ ] VMs criadas antes da mudança continuam listando e ligando (sem regressão
      em `domainToModel`).
- [ ] Commits pequenos e convencionais: `feat(vms): ...`, `test(vms): ...`.

---

## Sequência de commits sugerida

1. `feat(api): osType, virtioDriversIsoId e performanceProfile no contrato` — openapi.yaml + `./scripts/generate.sh`.
2. `feat(vms): gerador de XML de domínio por perfil de SO` — `buildDomainXML` puro + structs.
3. `feat(vms): feature detection de Hyper-V enlightenments via domcapabilities` + CD-ROM de drivers VirtIO + warnings.
4. `feat(vms): aplicar perfis no CreateVirtualMachine e expor performanceProfile em domainToModel`.
5. `feat(mock): suportar osType e performanceProfile no provider mock`.
6. `test(vms): testes table-driven do gerador de XML`.
7. `feat(ui): select de SO, ISO de drivers VirtIO, warnings e seção Perfil de performance`.
8. `docs: perfis de performance (ARCHITECTURE, ROADMAP, README)`.

## Observações

- A ISO do virtio-win é o que destrava a instalação: sem ela o Windows não vê o
  disco — por isso avisar em vez de falhar.
- O host de teste com QEMU ≤ 4.2 não suporta todos os enlightenments — por isso
  a detecção é pelas capabilities do host, não lista fixa.
