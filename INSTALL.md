# INSTALL.md — UltraV + CrimpCenter: P2V do XP e experimento no Windows 7

Este documento permite a **outro agente (ou humano)** reproduzir, do zero, a
instalação da máquina CrimpCenter/UltraV feita neste host, usando o backup
`Schleuniger.zip` e a imagem do disco do Windows XP.

Leia a seção **Resultado** primeiro: ela diz o que funciona e o que é beco sem saída.

---

## Resultado (leia antes de começar)

| Caminho | O que é | Estado |
|---|---|---|
| **A — P2V do XP** | O disco do XP original vira `qcow2` e roda como VM no libvirt | ✅ **Funciona** — XP boota, `chkdsk` corrige, PVI 2.5.3 inicializa |
| **B — Windows 7 x86 + EASY** | Win7 limpo + EASY copiado do backup | ⚠️ **Parcial** — EASY abre e chega ao login, mas **PVI não inicializa** (`VU1511`) |
| **C — Win7 + PVI moderno** | PVI 4.12/6.x no Win7 | ❌ **Impossível** — instalador recusa: *"At least Windows 10 1607 is required"* |

**Conclusão:** o alvo viável é o caminho **A**. O caminho B serve para provar que o
aplicativo roda no Win7, mas o runtime PVI 2.5.3 (de 2007) não inicializa nesse SO,
e não existe instalador público de uma versão compatível.

---

## 1. Pré-requisitos

### 1.1 Host

- Ubuntu 22.04+/26.04, x86_64, com `/dev/kvm` (KVM real)
- libvirt + QEMU + `qemu-utils` + `ntfs-3g` + `partx` (util-linux)
- Docker + Docker Compose v2, com o UltraV rodando (`deploy/docker-compose.yml`)
- Acesso SSH `felipe@<host>` com sudo sem senha
- UltraV servindo em `https://<host>:8275` (certificado autoassinado)
- Credenciais do UltraV: `admin` / valor de `ULTRAV_ADMIN_PASSWORD` no `deploy/.env`

### 1.2 Origem dos dados

| Item | Onde |
|---|---|
| Disco do XP (clonado) | anexado a uma máquina Windows (no caso: `ACC-MARIO`, `192.168.8.43`, como `D:`) |
| Backup do app | `d:\intel\Schleuniger.zip` nessa mesma máquina (1,68 GB) |
| Acesso a essa máquina | SSH por chave (usuário `accept`), com admin local |

### 1.3 Ferramentas usadas (host)

```bash
sudo apt-get install -y qemu-utils ntfs-3g util-linux mbr
# "mbr" fornece install-mbr (não foi o escolhido no fim, mas ajuda em testes)
```

---

## 2. Inventário de artefatos

No host, tudo fica em `/srv/ultrav-prensa/`:

```
/srv/ultrav-prensa/
├── app/
│   ├── Schleuniger.zip       # backup do programa instalado no XP (1,68 GB)
│   └── EASY.zip              # só a pasta Schleuniger/EASY (23 MB, para injeção offline)
├── pvi/                      # runtime B&R PVI 2.5.3 (pasta Bin, 34 arquivos)
├── hives/
│   ├── XP-SOFTWARE.hive      # HKLM\SOFTWARE do XP (inclui BR_Automation e Schleuniger)
│   └── XP-SYSTEM.hive        # HKLM\SYSTEM do XP
├── domain/
│   ├── xp-prensa.xml         # definição libvirt da VM do XP
│   └── mbr.bin               # 512 bytes do MBR original do disco (código de boot)
└── images/                   # (vazio) as imagens ficam em /var/lib/libvirt/images
    # xp-original.qcow2  149 GiB virtuais / 11,5 GiB reais  (BASE IMUTÁVEL)
    # xp-mbr.qcow2       149 GiB / 12 GiB                   (base reconstruída em MBR)
    # xp-work.qcow2      overlay em uso pela VM             (todo teste é descartável)
```

`MANIFEST.txt` traz o sha256 de cada artefato. Confira antes de usar.

---

## 3. Caminho A — P2V do Windows XP (o que funciona)

### A.1 Gerar a imagem do disco (na máquina Windows que tem o disco)

O `disk2vhd` (Sysinternals) **não aceita CLI** nas versões atuais — ele abre GUI.
Use o `wbadmin`, que é nativo e funcionou:

```bat
:: executar ELEVADO (admin). D: é o disco do XP.
wbadmin start backup -backupTarget:C: -include:D: -quiet
```

- Saída: `C:\WindowsImageBackup\<HOST>\Backup <data>\<guid>.vhdx`
- O `wbadmin` pode terminar com `EXIT=-4` **mesmo tendo gerado a imagem**. Verifique
  montando o VHDX antes de confiar (ver A.3).
- O disco de origem **não é modificado** (o `wbadmin` usa snapshot VSS).

### A.2 Trazer o VHDX para o host

Se o host não tiver chave SSH na máquina Windows, autorize-a:

```bash
# no host
ssh-keygen -t ed25519 -N "" -f ~/.ssh/id_ed25519   # se ainda não existir
cat ~/.ssh/id_ed25519.pub
```

Na máquina Windows (usuário administrador), o Windows OpenSSH lê
`C:\ProgramData\ssh\administrators_authorized_keys` — **não** o `%USERPROFILE%\.ssh\authorized_keys`
quando a conta é do grupo Administradores:

```powershell
# executar ELEVADO
$k = (Get-Content 'C:\hostkey.pub' -Raw).Trim()
Add-Content -Path 'C:\ProgramData\ssh\administrators_authorized_keys' -Value $k
```

Depois, do host:

```bash
scp -C accept@<windows>:'C:/WindowsImageBackup/<HOST>/Backup <data>/<guid>.vhdx' /home/felipe/xp.vhdx
qemu-img convert -p -O qcow2 /home/felipe/xp.vhdx /var/lib/libvirt/images/xp-original.qcow2
qemu-img info /var/lib/libvirt/images/xp-original.qcow2   # 149 GiB virtuais, ~11,5 GiB
```

### A.3 Verificar o conteúdo (sem montar partição)

O `wbadmin` gera **GPT** (partição MSR + dados). O NTLDR do XP **não boota de GPT** —
por isso o disco é reconstruído em MBR (A.4). Para inspecionar o conteúdo:

```bash
sudo qemu-nbd -r -c /dev/nbd2 /var/lib/libvirt/images/xp-original.qcow2
sudo qemu-io -r -c "read -v 16777216 64" /var/lib/libvirt/images/xp-original.qcow2
# deve mostrar "eb 52 90 4e 54 46 53" (VBR NTFS) no offset 0x01000000

sudo mkdir -p /mnt/xpo
sudo mount -t ntfs-3g -o ro,offset=16777216 /dev/nbd2 /mnt/xpo
ls /mnt/xpo        # WINDOWS, Schleuniger, "Program Files", ntldr, boot.ini...
sudo umount /mnt/xpo && sudo qemu-nbd --disconnect /dev/nbd2
```

> Use `-o offset=16777216` direto no dispositivo, **não** `/dev/nbdXpY`: em nbd
> read-only o `partx` falha e o nó de partição fica inválido.

### A.4 Reconstruir o disco em MBR (passo crítico)

Valores do disco original (medidos):

```
disco: 160194429952 bytes = 312.879.746 setores de 512
partição XP: início 32768 setores (16.777.216 bytes)
             tamanho 312.576.642 setores
```

```bash
# 1) imagem nova, MBR, uma partição ativa
sudo qemu-img create -f qcow2 /var/lib/libvirt/images/xp-mbr.qcow2 160194429952
sudo qemu-nbd -c /dev/nbd1 /var/lib/libvirt/images/xp-mbr.qcow2
echo "start=32768,size=312576642,type=7,bootable" | sudo sfdisk --label dos /dev/nbd1

# 2) copiar SÓ os blocos alocados (não use ntfsclone: ele falha no $Bitmap)
sudo qemu-nbd -r -c /dev/nbd0 /var/lib/libvirt/images/xp-original.qcow2
python3 - <<'PY'
import json, subprocess
m = json.loads(subprocess.check_output(['sudo','-n','qemu-img','map','--output=json',
      '/var/lib/libvirt/images/xp-original.qcow2']))
PART_OFF, PART_LEN = 32768*512, 312576642*512
for e in m:
    if not e.get('data'): continue
    a = max(e['start'], PART_OFF)
    b = min(e['start']+e['length'], PART_OFF+PART_LEN)
    if b <= a: continue
    # ATENÇÃO: seek ABSOLUTO (a), porque escrevemos no disco inteiro (/dev/nbd1)
    subprocess.check_call(['sudo','-n','dd','if=/dev/nbd0','of=/dev/nbd1',
        'bs=1M','iflag=skip_bytes,count_bytes','oflag=seek_bytes','conv=notrunc',
        f'skip={a}', f'seek={a}', f'count={b-a}'],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
print('ok')
PY

# 3) MBR original do XP (o do install-mbr é interativo e trava em "MBR")
#    Obtenha os 512 bytes do disco físico:
#    PowerShell ELEVADO:
#      $fs=New-Object IO.FileStream('\\.\PhysicalDrive0',[IO.FileMode]::Open,[IO.FileAccess]::Read)
#      $b=New-Object byte[] 512; $null=$fs.Read($b,0,512); $fs.Close()
#      [IO.File]::WriteAllBytes('C:\mbr.bin',$b)
#    Depois:
sudo dd if=/home/felipe/mbr.bin of=/dev/nbd1 bs=1 count=440 conv=notrunc
sudo dd if=/dev/nbd1 bs=1 count=8 2>/dev/null | xxd     # deve começar 33c0 8ed0

# 4) corrigir HIDDEN SECTORS do VBR (senão o NTLDR não lê o resto: CPU 100% e tela preta)
sudo dd if=/dev/nbd1 bs=1 skip=$((16777216+28)) count=4 2>/dev/null | xxd
#    valor antigo: 3f 00 00 00 (63). Correto: 00 80 00 00 (32768)
printf '\x00\x80\x00\x00' | sudo dd of=/dev/nbd1 bs=1 seek=$((16777216+28)) conv=notrunc

sudo qemu-nbd --disconnect /dev/nbd1
sudo qemu-nbd --disconnect /dev/nbd0
```

> **Se pular o passo 4**, o sintoma é exatamente: SeaBIOS "Booting from Hard Disk...",
> tela preta, qemu em 100% de CPU e ~18 setores lidos no total.

### A.5 Overlay + domínio

```bash
# a base (xp-mbr) nunca é escrita: a VM usa um overlay
sudo qemu-img create -f qcow2 -b /var/lib/libvirt/images/xp-mbr.qcow2 \
     -F qcow2 /var/lib/libvirt/images/xp-work.qcow2
sudo virsh define /srv/ultrav-prensa/domain/xp-prensa.xml
sudo virsh start xp-prensa
```

XML essencial (o UltraV só gera SATA/scsi; para XP é preciso IDE — por isso `virsh`):

```xml
<domain type='kvm'>
  <name>xp-prensa</name>
  <memory unit='KiB'>1572864</memory><vcpu>1</vcpu>
  <os><type arch='i686' machine='pc'>hvm</type><boot dev='hd'/></os>
  <features><acpi/><apic/><pae/></features>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2'/>
      <source file='/var/lib/libvirt/images/xp-work.qcow2'/>
      <target dev='hda' bus='ide'/>
    </disk>
    <interface type='network'><source network='default'/><model type='rtl8139'/></interface>
    <graphics type='vnc' autoport='yes'><listen type='socket'/></graphics>
    <video><model type='cirrus' vram='16384' heads='1'/></video>
  </devices>
</domain>
```

> **VNC em socket é obrigatório para o console do UltraV** (`<listen type='socket'/>`).
> Se usar `listen='127.0.0.1'` (TCP), a UI mostra console **preto** — o backend
> conecta em `/var/lib/libvirt/qemu/domain-*/vnc.sock` e o fallback
> `OpenGraphicsFD` falha no deploy em container.

### A.6 Primeiro boot

- O XP roda `chkdsk` (o volume vem "sujo" do clone) — normal, ~2 min. Ele corrige
  `$Bitmap` e reinicia.
- Aparece **Windows Product Activation**: *"hardware changed… reactivate within 3 days"*.
  A ativação online do XP está desligada → use **ativação por telefone**.
- Login: a conta original da máquina (senha do operador).

### A.7 Snapshot

```bash
sudo virsh snapshot-create-as xp-prensa xp-boot-ok "primeiro boot OK"
```

---

## 4. Caminho B — Windows 7 x86 + EASY (experimental)

Serve para demonstrar que o aplicativo roda no Win7. **Não** entrega um sistema de
produção, porque o PVI não inicializa.

### B.1 Criar a VM (API do UltraV)

```bash
B=https://<host>:8275
TOKEN=$(curl -sk -X POST $B/api/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"<senha>"}' | jq -r .token)

# ISO de instalação precisa estar na library (POST multipart /api/v1/storage/isos)
curl -sk -X POST $B/api/v1/vms -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{
    "name":"win7-lab","vcpus":2,"memoryBytes":3221225472,
    "disk":{"poolId":"default","sizeBytes":42949672960,"format":"qcow2"},
    "networkId":"default","osType":"other","nicModel":"e1000e",
    "isoId":"<iso-do-windows-7>.iso","start":false}'
```

- `osType: other` → **SATA + e1000e**: o setup do Win7 vê o disco e a rede **sem drivers**.
- A criação devolve `202` + task; acompanhe em `GET /api/v1/tasks/{id}`.

### B.2 Instalar o Windows 7 e o Bitvise

- Instalação padrão (Pt-BR, Professional x86).
- Bitvise SSH Server: instalar; ele **não inicia o serviço** na instalação — reinicie a
  VM ou clique *Start Server*.
- Liberar a porta 22 (o firewall do Windows bloqueia, e a regra do instalador pode ser
  limitada à sub-rede local):

```bat
netsh advfirewall firewall add rule name=ssh22 dir=in action=allow protocol=tcp localport=22
```

- Acesso externo: `DNAT` no host `<host>:2222 -> <ip-da-vm>:22` + reserva DHCP do MAC.
  Documentado em `deploy/` (script `/usr/local/sbin/ultrav-vm-forward.sh` + systemd).

### B.3 Copiar o EASY

```bash
# no host: extrair só a pasta do app
unzip -q /srv/ultrav-prensa/app/Schleuniger.zip 'Schleuniger/EASY/*' -d /tmp/easy
# enviar para a VM (Bitvise SFTP)
sshpass -p '<senha>' scp -r -P 2222 /tmp/easy/Schleuniger/EASY accept@<host>:'C:/Schleuniger/'
```

Sem SSH disponível, a alternativa é **offline**: parar a VM, `qemu-nbd` + `ntfs-3g`
e copiar direto em `C:\`.

### B.4 Runtime PVI (o ponto que falha)

O EASY importa `PviCom.dll` e espera o PVI Manager. Replicar do XP:

1. Copiar a **árvore completa** `C:\Program Files\BrAutomation` (61 arquivos, 11 MB):
   `Bin\`, `BrSetup\`, `Help\`, `Log\`, `PVI\`.
2. Copiar `PviCom.dll` e `PviLog.dll` também para `C:\Windows\System32`.
3. Importar o registro do XP (`HKLM\SOFTWARE\BR_Automation` e `HKLM\SOFTWARE\Schleuniger`):

```bash
# no host, com as hives do XP
sudo reged -x /srv/ultrav-prensa/hives/XP-SOFTWARE.hive 'HKEY_LOCAL_MACHINE\SOFTWARE' 'BR_Automation' /tmp/br_auto.reg
sudo reged -x /srv/ultrav-prensa/hives/XP-SOFTWARE.hive 'HKEY_LOCAL_MACHINE\SOFTWARE' 'Schleuniger'   /tmp/schleu.reg
# transferir e, no Windows: reg import C:\br_auto.reg
```

4. **Iniciar o PviMan com o diretório de trabalho da pasta Bin** (senão ele falha com
   `PVI Error 12103`):

```bat
start "" /d "C:\Program Files\BrAutomation\PVI\V2.5.3\Bin" "C:\Program Files\BrAutomation\PVI\V2.5.3\Bin\PviMan.exe"
timeout /t 6 >nul
start "" /d "C:\Schleuniger\EASY" "C:\Schleuniger\EASY\EASY.exe"
```

**Resultado observado:** `PviMan` sobe e fica no tray, `EASY` abre e mostra a tela de
login, mas reporta **"Inicialização PVI errada"** e o código `VU1511`. O log do PVI
(`Log\PviMan.log`) fica vazio. Não foi possível destravar.

### B.5 Por que o caminho B não fecha

| Versão do PVI | Requer | Serve no Win7? |
|---|---|---|
| **2.5.3** (a que o EASY espera) | XP | ❌ não inicializa; **não há instalador público** |
| **4.12.11.3** | Windows 10 1607+ | ❌ o instalador recusa: *"At least Windows 10 1607 is required"* |
| 6.5 / 6.7 | Windows 10/11 | ❌ mais restritivo |

A única combinação que funciona é **EASY 3.36 + PVI 2.5.3 + PLC antigo**, exatamente o
que está na imagem do XP — ou seja, caminho A.

---

## 5. Armadilhas (todas custaram tempo)

| Sintoma | Causa | Solução |
|---|---|---|
| `disk2vhd` abre GUI e ignora args | CLI removida nas versões atuais | usar `wbadmin start backup` |
| `wbadmin` termina `EXIT=-4` | erro no fechamento, imagem gerada | montar o VHDX e validar antes de usar |
| NTLDR não boota a imagem | VHDX do `wbadmin` é **GPT** | reconstruir disco **MBR** (A.4) |
| `ntfsclone` recusa | `$Bitmap` inconsistente / volume "sujo" | copiar blocos alocados via `qemu-img map` |
| SeaBIOS mostra "MBR" e para | `install-mbr` é interativo | usar o **MBR original** do disco (440 bytes) |
| Tela preta, qemu 100% CPU, ~18 setores lidos | VBR com **hidden sectors** errado | patch em `VBR+0x1C` para o início real |
| Console do UltraV preto | VNC em **TCP** no domínio | `<listen type='socket'/>` (A.5) |
| `PATCH /vms/{id} {"isoId"}` erro 500 | target do CD-ROM colidia com o disco | corrigido no backend (`update.go`) |
| ISO anexada depois não boota | `<os><boot>` do libvirt vs boot por device | corrigido no backend (reescreve a ordem) |
| Bitvise recusa senha | conta sem senha / firewall / serviço parado | `net user <u> *`, regra `ssh22`, reiniciar a VM |
| `capabilities.kvm = false` em host KVM | tag auto-fechada `<domain type='kvm'/>` | corrigido no backend (`host.go`) |

### Correções feitas no código do UltraV (já no working tree)

- `backend/internal/hypervisor/libvirt/host.go` — detecção de KVM no XML do libvirt 12
- `backend/internal/hypervisor/libvirt/update.go` — target de CD-ROM livre + caminho
  absoluto do ISO + ordem de boot ao anexar mídia depois

---

## 6. Checklist de verificação

Caminho A:

- [ ] `qemu-img info xp-original.qcow2` → 149 GiB virtuais
- [ ] `qemu-io -r -c "read -v 16777216 64"` → `eb 52 90 4e 54 46 53`
- [ ] `dd if=<imagem> bs=1 count=8` → `33c0 8ed0` (MBR do XP, após A.4)
- [ ] `dd skip=16777244 count=4` → `00 80 00 00` (hidden sectors = 32768)
- [ ] `virsh domstate xp-prensa` → running
- [ ] console do UltraV mostra o XP (não preto)
- [ ] `virsh snapshot-list xp-prensa` → `xp-boot-ok`
- [ ] EASY abre e o PVI inicializa (sem "Inicialização PVI errada")

---

## 7. USB da prensa (próximo passo)

O PC antigo fala com a máquina por **USB** (interface B&R). Para a VM:

```bash
lsusb                                   # identificar vendor:product
# hostdev USB no domínio (persistente)
cat > /tmp/usb.xml <<'XML'
<hostdev mode='subsystem' type='usb' managed='yes'>
  <source><vendor id='0xVVVV'/><product id='0xPPPP'/></source>
</hostdev>
XML
sudo virsh attach-device xp-prensa /tmp/usb.xml --config
```

- A VM precisa ser reiniciada para o device aparecer.
- Se o device já estiver em uso pelo host, remova do host antes.
- No XP, o driver já vem da instalação original (é a mesma interface que a máquina usava).

---

## 8. Referências no repositório

- `AGENTS.md` — instalação do UltraV no host (Docker, libvirt, GIDs)
- `deploy/docker-compose.yml` + `deploy/.env.example` — stack do UltraV
- `docs/api/openapi.yaml` — contrato da API usada nos passos B.1
