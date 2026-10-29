# Formato `.ord` do Schleuniger EASY — referência para integração

Documento derivado de **evidência real** na instalação do CrimpCenter (EASY 3.36.15.4):

- template oficial: `system/empty.ord` (2 KB, todas as chaves vazias)
- ordem real de produção: `system/tempOrder.ord` (13 KB, ordem "circuito", 2019)

Serve para quem quiser **gerar ordens automaticamente** (ERP/MES/PCP) e entregá-las à máquina.

> **Aviso:** os nomes de campo e a estrutura são evidência direta dos arquivos. O
> **significado** de cada campo é inferido do nome (alemão) e dos valores; a **escala**
> dos números precisa ser calibrada (ver §5). Nada aqui substitui a documentação do
> fabricante nem validação com a máquina.

---

## 1. O que é

Uma ordem de produção do EASY é um arquivo **texto puro**, formato INI, com seções
hierárquicas por `.` (ex.: `[ORDER.ARTICLE.LINE.WIRE]`). Contém o cabeçalho da ordem,
o artigo (fio + comprimentos), os ajustes de linha, o cadastro elétrico do fio, o
descasque por ponta, tolerâncias/correções e os registros de qualidade.

## 2. Codificação e versionamento

| Item | Valor observado |
|---|---|
| Codificação | **ISO-8859-1** (Latin-1) — tem `PRIORITÄT` com trema |
| Fim de linha | **CRLF** (`\r\n`) |
| Versão do formato | `[FILEINFO] VERSION=` (a ordem real: `3.36.15.4`, igual à versão do EASY; o template trazia `3.3.3.10`) |
| Revisão | `[FILEINFO] REVISION=` (ex.: `322`) |
| Checksum | última seção `[$CHECKSUMME$] sum=` — gerado pelo EASY |

Se um sistema gerar o arquivo, o caminho mais seguro é **partir de uma ordem exportada
pelo próprio EASY** e substituir os valores (garante versão, checksum e campos novos).

## 3. Árvore de seções (33 na ordem real)

| Seção | Chaves | Papel |
|---|---|---|
| `[FILEINFO]` | 2 | versão/revisão do formato |
| `[ORDER]` | 38 | cabeçalho: ID, cliente, quantidade, prioridade, status, datas |
| `[ORDER.ARTICLE]` | 6 | comprimentos do artigo (`LEITUNGSLAENGE`, `_2`) |
| `[ORDER.ARTICLE.LINE]` | 15 | ajustes da linha: faca, offsets, grippers, coiler |
| `[ORDER.ARTICLE.LINE.INCJETPRINTLAYOUT]` (e `_2`) | 14 | layout de marcação inkjet (fonte, orientação, tensão) |
| `[ORDER.ARTICLE.LINE.CUTTER]` | 6 | tipo/raio da faca, run-over-strip |
| `[ORDER.ARTICLE.LINE.WIRE]` | 53 | **cadastro do fio**: bitola, diâmetros, velocidades, pressões |
| `[ORDER.ARTICLE.LINE.WIRE.CUTTER]` | 6 | faca específica do fio |
| `[ORDER.ARTICLE.LINE.WIRE.COLOR]` | 3 | cor principal/secundárias |
| `[ORDER.ARTICLE.LINE.MIDSTRIPPINGLAYOUT]` | 8 | descasque no meio do fio |
| `[ORDER.ARTICLE.LINE.SCHLITZERLAYOUT]` | 8 | corte longitudinal (slitting) |
| `[ORDER.ARTICLE.LINE.HOTSTAMPLAYOUT]` | 8 | marcação a quente |
| `[ORDER.ARTICLE.WIREEND_0/1/2]` | 3 cada | **descasque por ponta** |
| `[ORDER.ARTICLE.MIDSTRIPPINGPOINTS]` | 1 | pontos de descasque intermediário |
| `[ORDER.PRODUCTION]` | **183** | correções e tolerâncias de produção |
| `[ORDER.PRODUCTION.{CH,IH,AZK}MEASUREMENTVALUES_WIREEND{0,1,2}]` | 10 cada | registros de medição/qualidade, com `CRIMPID` |
| `[ORDER.PRODUCTION.MEASUREMENTDATA]` | 10 | medição consolidada |
| `[ORDER.INCJETTEXT]` (e `_2`) | 27 | textos de marcação inkjet |
| `[ORDER.HOTSTAMPTEXT]` | 25 | textos de marcação a quente |
| `[ORDER.LABELTEXT]` | 10 | textos de etiqueta |
| `[$CHECKSUMME$]` | 1 | checksum do arquivo |

## 4. Campos que interessam para "mandar o setup"

### 4.1 Cabeçalho — `[ORDER]`

| Campo | Exemplo | Significado |
|---|---|---|
| `ID` | `circuito` | identificador da ordem |
| `DATEINAME` | `SA1:Gruppe 0\circuito.ord` | caminho/origem da ordem |
| `CUSTOMERNR`, `GHW_NR`, `SCAN_CODE` | — | cliente, número interno, código de barras |
| `PRIORITÄT`, `ORDERPOSITION` | 0 | prioridade e posição na fila |
| `STUECKZAHL_SOLL` | 4 | **quantidade alvo** |
| `LOSGROESE_SOLL` | 4 | **tamanho do lote** |
| `NETPRODUCTIONSTATUS` | 1 | status reportado pela interface de rede |
| `CREATED`/`CHANGED`/`FINISHED` | `21.03.2019 14:59:51:000` | datas (ms) |
| `WIRELISTPRODTYP` | 1 | tipo de produção da lista de fios |

### 4.2 Artigo — `[ORDER.ARTICLE]`

| Campo | Exemplo | Significado |
|---|---|---|
| `LEITUNGSLAENGE` | 5150000 | comprimento do fio A (≈ 515 mm — ver §5) |
| `LEITUNGSLAENGE_2` | 1400000 | comprimento do fio B (≈ 140 mm) |

### 4.3 Linha — `[ORDER.ARTICLE.LINE]`

| Campo | Exemplo | Significado |
|---|---|---|
| `MESSER_OFFSET` | 0 | offset da faca |
| `OFFSETWINKEL_0/1`, `OFFSETABSTAND_0/1` | 0 | ângulos/distâncias de offset |
| `ANZ_VERBINDUNGSFEHLER` | 3 | nº de falhas de conexão toleradas |
| `MESSINTERVALL` | 10 | intervalo de medição |
| `HAUBENZWISCHENSTUFE` | 1000 | estágio intermediário da coifa |
| `WZ_ABZUGGREIFER_OEFFNEN` / `_SCHLIESSEN` | 50 / 30 | posições do gripper de tração |
| `ISCABLECOILER` / `ISABBINDEEINHEIT` | 0 | usa enrolador / unidade de amarração |

### 4.4 Fio — `[ORDER.ARTICLE.LINE.WIRE]` (o cadastro elétrico)

| Campo | Exemplo | Significado |
|---|---|---|
| `ID` / `NAME` | `CABO 0,75MM ESP.REDUZIDO` | identificação do fio |
| `DATEINAME` | `SA1:Gruppe20\....wir` | cadastro `.wir` de origem |
| `QUERSCHNITT` | 75 | **bitola** (0,75 mm²) |
| `QUERSCHNITT_AWG` / `QUERSCHNITT_EINHEIT` | 19 / 0 | AWG e unidade |
| `LEITERDURCHMESSER` | 85000 | diâmetro do condutor (0,85 mm) |
| `ISOLATIONDURCHMESSER` | 150000 | diâmetro da isolação (1,5 mm) |
| `MANTELDURCHMESSER` | 185000 | diâmetro externo (1,85 mm) |
| `ANZAHLLITZEN` | 24 | número de fios |
| `KLEMMDRUCK`, `GESCHWINDIGKEIT`, `BESCHLEUNIGUNG`, `VERZOEGERUNG` | 25 / 35 | pressão de garra e perfil de movimento |
| `WZ_N_GREIFER_SCHLIESSEN`, `WZ_N_KABEL_SCHNEIDEN`, `WZ_N_ISOLATION_SCHNEIDEN` | 50 | posições de ferramenta |
| `MESSERLUEFTENMANTEL`, `IMESSERLUEFTEN` | 10000 / 5000 | folga da faca |
| `LAENGEOFFSET`, `OEFFNUNG` | 100000 / 1000 | offset de comprimento, abertura |
| `LAENGETOTAL` / `LAENGETOTAL_GENUTZT` | 560083870000 | total e total já usado (contadores) |
| `ZUGKRAFT` | 100 | força de tração |
| flags | `SPLICECONTROL`, `SPARKTESTER`, `ISPSU`, `BINDEN`, `BEIDSEITIGKLEBEN`… | opcionais de processo |

### 4.5 Descasque por ponta — `[ORDER.ARTICLE.WIREEND_n]`

| Campo | Ex. ponta 0 / 1 / 2 | Significado |
|---|---|---|
| `ABISOLIERLAENGE` | 40000 / 35000 / 40000 | **comprimento de descasque** (≈ 4 / 3,5 / 4 mm) |
| `MANTELABZUG` | 0 / 0 / 10000 | retirada de capa (≈ 1 mm na ponta 2) |
| `TEILABZUG` | 0 | puxada parcial |

### 4.6 Produção — `[ORDER.PRODUCTION]` (183 chaves)

É aqui que ficam o **ajuste fino e a qualidade** — o que o operador costuma mexer na tela:

| Grupo | Campos | Uso |
|---|---|---|
| Correções de comprimento | `LEITUNGA/B_KORREKTURLAENGE`, `ARABISOLIER_KORREKTURLAENGE_n`, `ARCRIMP_KORREKTURLAENGE_n`, `ARSEAL_KORREKTURLAENGE_n` | compensação por elemento |
| Tolerâncias | `ATOLPLUS_n`, `ATOLMINUS_n`, `VTOLPLUS_n`, `VTOLMINUS_n`, `FILTER_n` (n=0..3) | janelas de aceitação |
| Resultado | `CRIMPRESULT_n`, `CRIMPMESSAGE_n`, `CPK_n` | qualidade por crimp |
| Amostra | `MUSTER_STUECKZAHL`, `MUSTER_KABELLAENGE`, `MUSTER_KABELPRODTYP` | peça de referência |
| Processo | `TRANSPORTBAND`, `GETAKTETESBAND_MS`, `EINRICHTEN_OPT_KABELVERBR` | esteira e setup |

### 4.7 Medição por ponta — `[ORDER.PRODUCTION.{CH,IH,AZK}MEASUREMENTVALUES_WIREENDn]`

Cada ponta tem até três tipos de medição (`CH`, `IH`, `AZK`), com 10 campos cada:
`MASCHSIDE`, `MESSUNGOK`, `MEASUREMENTDATE` e **`CRIMPID`** (ferramenta/aplicador usado
naquela medição). Exemplo real: ponta 0 → `CRIMPID=3385`; ponta 1 → `6938`; ponta 2 → `5606`.

> O `CRIMPID` aparece **nos registros de medição**, não no cadastro do artigo. A
> atribuição da ferramenta ao artigo vem do cadastro do produto/máquina.

## 5. Unidades (fixed-point) — calibrar antes de gerar

Os números são inteiros em ponto fixo. Pelos valores reais, as **escalas provavelmente são**:

| Campo | Valor | Interpretação | Fator |
|---|---|---|---|
| `QUERSCHNITT` | 75 | 0,75 mm² | ×0,01 |
| `LEITERDURCHMESSER` | 85000 | 0,85 mm | ×0,00001 |
| `ISOLATIONDURCHMESSER` | 150000 | 1,5 mm | ×0,00001 |
| `MANTELDURCHMESSER` | 185000 | 1,85 mm | ×0,00001 |
| `ABISOLIERLAENGE` | 40000 | ≈ 4 mm | ×0,0001 |
| `LEITUNGSLAENGE` | 5150000 | ≈ 515 mm | ×0,0001 |
| `MESSERLUEFTENMANTEL` | 10000 | ≈ 1 mm | ×0,0001 |

**Não confie nisso sem calibração.** Procedimento recomendado:

1. No EASY, criar uma ordem com valores redondos conhecidos (ex.: fio 500 mm, descasque 5 mm).
2. Exportar a ordem e ler os inteiros.
3. Deduzir o fator por campo (comprimentos, diâmetros e bitola podem usar escalas diferentes).
4. Validar com uma produção real de 1 peça antes de liberar.

## 6. Ordem mínima viável

Partindo de uma ordem exportada, o mínimo a preencher:

```ini
[FILEINFO]
VERSION=<versão do EASY instalado>

[ORDER]
ID=<id da ordem>
STUECKZAHL_SOLL=<quantidade>
LOSGROESE_SOLL=<lote>

[ORDER.ARTICLE]
LEITUNGSLAENGE=<comprimento A>
LEITUNGSLAENGE_2=<comprimento B, se houver>

[ORDER.ARTICLE.LINE.WIRE]
ID=<nome do fio>          ; deve existir no cadastro da máquina (.wir)
QUERSCHNITT=<bitola>
LEITERDURCHMESSER=<...>
ISOLATIONDURCHMESSER=<...>
MANTELDURCHMESSER=<...>

[ORDER.ARTICLE.WIREEND_0]
ABISOLIERLAENGE=<descasque ponta 0>
[ORDER.ARTICLE.WIREEND_1]
ABISOLIERLAENGE=<descasque ponta 1>
```

Não invente `[$CHECKSUMME$]`: gere a ordem no EASY (ou copie o algoritmo) e altere
apenas valores, deixando o EASY recalcular.

## 7. Como entregar a ordem à máquina

| Canal | Como funciona | Observações |
|---|---|---|
| **EasyNet (TCP 9901)** | `system/EasyNet.ini`: `[SERVER] PORT=9901`, `ACTIVE=1`, `PRODSTATUS=1`, `PARAMORDER=1`, `TIMEOUT=4000` | Canal previsto para host/ERP: aceita ordens parametrizadas e reporta status. **Precisa da doc do protocolo (Schleuniger).** |
| HTTP (opcional) | `[HTTPSERVER] PORT=80 ACTIVE=0` | Desativado nesta instalação |
| **Arquivo** | gerar o `.ord` e disponibilizar na pasta de ordens/`Import` | Simples, sem protocolo; o operador seleciona a ordem na tela |
| Export/Import | `Export\` / `Import\` (o `Import` aceitou um `.ebf` = arquivo de configuração/fábrica) | .ebf ≠ ordem: é backup de configuração |

## 8. O que o formato **não** resolve

- **Setup mecânico**: montar aplicador, ajustar altura de crimpagem no aplicador, guias,
  lâminas — continua físico/manual.
- **Cadastro de ferramentas** (`CRIMPID`) e cadastro de fios (`.wir`): vêm do cadastro
  da máquina, não da ordem.
- **Validação de segurança/qualidade**: enviar parâmetros automaticamente exige
  validação (tolerâncias, aprovação de amostra) — é processo, não arquivo.

## 9. O que pedir à Schleuniger

1. Documentação do **EasyNet / host interface** (formato das mensagens, handshake, porta,
   semântica de `PARAMORDER` e `PRODSTATUS`).
2. Especificação oficial do `.ord` (versão 3.36) — escalas e campos obrigatórios.
3. Se há **modo automático** de aceitação de ordem (sem confirmação do operador) e como
   habilitar com segurança.
