# Pla d’implementació dels paràmetres de petició per perfil

**Data:** 2026-08-11

**Estat:** implementat i verificat

**Nivell:** CRITICAL

**Especificació canònica:** [Fase de paràmetres de petició per perfil de model](../specs/2026-08-11-provider-request-parameters-phase-design.md)

## 1. Resultat i acceptació vinculada

El resultat és un únic camí configurable des del perfil `[[models]]` fins al body JSON de Chat Completions, amb omissió real dels camps absents i amb Shellia mantenint l’autoritat sobre el transport i la resposta que sap consumir.

El pla agrupa l’acceptació de l’especificació en cinc resultats demostrables:

- **AC1 — Default del proveïdor:** un perfil sense `request_params` no envia `temperature` ni cap camp addicional. Cobreix §1, §5.3 i §7.1.
- **AC2 — Passthrough per perfil:** scalars, arrays i taules niades arriben al proveïdor amb la mateixa forma JSON i no s’hereten entre perfils. Cobreix §3.1, §4.1 i §7.1.
- **AC3 — Autoritat i fallada primerenca:** tots els perfils es validen abans de fer HTTP; claus protegides i valors no JSON-compatible fallen amb perfil i ruta, i els camps de Shellia no es poden sobreescriure. Cobreix §4.2–§4.4 i §7.2.
- **AC4 — Selecció coherent:** selecció inicial, `/model` i overrides existents mantenen un conjunt de paràmetres coherent amb el perfil seleccionat i la persistència conserva les taules. Cobreix §5.1–§5.2.
- **AC5 — Migració i diagnòstic honestos:** Luna deixa de rebre el `temperature: 0` implícit, els 4xx continuen visibles, els valors arbitraris no entren en traces ni consumidors aliens, i la documentació explica compatibilitat i riscos. Cobreix §5.3, §6 i §9.4.

La implementació s’atura quan AC1–AC5 disposen de prova automatitzada, la documentació coincideix amb el comportament i la seqüència final és verda.

## 2. Nivell i proporcionalitat

Es conserva el nivell **CRITICAL** de l’especificació perquè es modifica una frontera de proveïdor i s’introdueix configuració arbitrària que pot afectar cost, privacitat i forma de resposta. No canvien la política local de seguretat, la concurrència ni l’estat durable.

La solució queda limitada a tres slices verticals sobre propietaris existents. No s’afegeixen dependències, esquemes de proveïdor, registries, CLIs, serveis ni abstraccions reutilitzables. La validació nova existeix només perquè el body configurable no pugui trencar el contracte que Shellia consumeix.

Baseline verificat en redactar el pla:

```text
env GOCACHE=/tmp/go-build go test -count=1 ./internal/config ./internal/app ./internal/llm
```

Resultat: els tres paquets passen amb els canvis locals previs preservats.

## 3. Abast

### Dins d’abast

- `request_params` opcional i propi de cada perfil;
- valors TOML recursius compatibles amb JSON, sense dates ni floats no finits;
- projecció estreta cap a `llm.ClientOptions`;
- fusió final i claus protegides propietat d’`internal/llm`;
- validació de tots els perfils abans de seleccionar-ne un;
- omissió del `temperature` implícit;
- canvi de paràmetres amb `/model` i caracterització dels overrides existents;
- proves focalitzades de parser, projecció, body, reintents, errors i persistència;
- actualització mínima de README, plantilla i web pública;
- aclariment de la política de traces i compatibilitat amb binaris antics.

### Fora d’abast

- paràmetres globals, herència o overrides nous;
- validació semàntica per model o proveïdor;
- secrets interpolats, headers, query parameters o endpoints configurables;
- `null`, streaming, múltiples choices, tools, cerca remota, àudio o multimodalitat;
- Responses API o APIs no compatibles amb Chat Completions;
- nous límits de mida o profunditat per a configuració local;
- redisseny de traces o de la UI.

### Diferit

Es manté exactament el treball diferit de §10 de l’especificació. Cap element diferit és prerequisit dels cinc criteris actuals.

## 4. Propietaris i punts de reutilització

- **Representació i càrrega:** `internal/config.Config`, `ModelConfig`, `FileModelConfig`, `applyFileConfig`, `normalizeModelConfigs` i la comprovació `toml.MetaData.Undecoded` existent.
- **Selecció i validació d’arrencada:** `internal/app.finalizeConfig`, `validateModelConfigs`, `applyModelConfig` i `switchInteractiveModel`.
- **Projecció estreta:** `internal/app.llmClientOptions`; UI, executor i trace conserven les seves projeccions actuals sense rebre el mapa.
- **Contracte extern:** `internal/llm.ClientOptions`, `chatCompletionRequest`, `callPlanningPrompt` i `doLLMRequest`.
- **Persistència:** `internal/config.persistDefaultModel`, que canvia només `default_model` i conserva el contingut restant.
- **Prova HTTP:** el fake LLM existent a `internal/app/main_loop_test.go`, que captura els bodies reals.
- **Documentació activa:** plantilla de `internal/config/config.go`, seccions de perfils i traces de `README.md`, i exemple públic de `site/index.html`.

`internal/llm` serà l’únic propietari de la llista de claus protegides i de la validació del contracte del body. `internal/app` només invocarà aquesta validació per tots els perfils; `internal/config` no duplicarà coneixement de Chat Completions.

## 5. Ordre d’execució

```text
Slice 1: perfil → body i omissió del default
    ↓
Slice 2: autoritat, tipus i fallada primerenca
    ↓
Slice 3: selecció interactiva, migració i contracte públic
```

El segon slice necessita la representació i el camí HTTP del primer. El tercer caracteritza el comportament efectiu i documenta un contracte ja protegit; no pot precedir els dos anteriors.

## 6. Slice 1 — Perfil fins al body JSON

### Resultat avançat

Entrega AC1 i la base d’AC2: un perfil pot afegir camps al body, mentre un perfil buit usa els defaults del proveïdor. El `temperature: 0` deixa de formar part del contracte intern.

### Owner i límit afectat

- `internal/config`: representació i normalització per perfil;
- `internal/app`: aplicació del perfil i projecció estreta;
- `internal/llm`: construcció d’un body nou i serialització.

### Dependència

Cap canvi funcional previ. Parteix del baseline verd i de la selecció de perfils existent.

### Treball executable

1. Afegir primer proves RED que demostrin:
   - un `[models.request_params]` amb string, bool, enter, float, array i taula niada es carrega dins del perfil correcte;
   - `llmClientOptions` rep només els paràmetres del perfil actiu;
   - una petició sense paràmetres omet `temperature`;
   - una petició configurada conserva exactament la forma JSON.
2. Estendre `FileModelConfig`, `ModelConfig` i la configuració activa amb un mapa opcional, preservant `nil` o buit com a absència.
3. Propagar el mapa només mitjançant `applyModelConfig` i `llmClientOptions`. No afegir-lo a opcions d’UI, executor, sessió o trace.
4. Fer que `internal/llm` creï un objecte de petició nou, copiï els camps addicionals i hi escrigui `model`, `messages` i el `response_format` aplicable.
5. Retirar `Temperature` del request fix i de l’àlies intern de prova. Adaptar `DoRequest` només en la mesura necessària per conservar el seu ús actual de tests.
6. Confirmar que el mapa del perfil no es muta durant construcció, serialització ni reintents.

### Verificació proporcional

- RED/GREEN a `internal/config` per descodificació i aïllament entre perfils.
- RED/GREEN a `internal/app`/`internal/llm` capturant el body exacte amb i sense paràmetres.
- Regressió de `response_format` present i omès segons `supports_response_format`.
- Prova que dos intents d’una mateixa petició utilitzen bytes idèntics.

Comanda de tancament del slice:

```text
env GOCACHE=/tmp/go-build go test -count=1 ./internal/config ./internal/app ./internal/llm
```

**Done evidence:** els tests capturen un body sense `temperature` per defecte i un body amb paràmetres scalars/niats exactes, sense mutació del perfil.

## 7. Slice 2 — Autoritat i validació primerenca

### Resultat avançat

Entrega AC3 i completa AC2: la flexibilitat queda limitada a camps que no trenquen el transport ni la resposta textual única, i qualsevol perfil invàlid falla abans de contactar un proveïdor.

### Owner i límit afectat

- `internal/llm`: conjunt canònic de claus protegides, validació recursiva i defensa final de la fusió;
- `internal/app`: validació de tots els perfils durant la finalització de configuració;
- `internal/config`: conserva el parser estricte fora de `request_params`.

### Dependència

Slice 1 verd; la validació opera sobre la representació i el camí de dades ja funcionals.

### Treball executable

1. Afegir proves RED tabulars per cada categoria de claus protegides definida a §4.3 de l’especificació, comprovant coincidència exacta al nivell superior i absència de falsos positius per claus niades o amb capitalització diferent.
2. Afegir proves RED recursives per dates/hores TOML, `nan`, infinits i qualsevol valor no serialitzable; l’error ha d’incloure perfil i ruta, però no el valor.
3. Definir a `internal/llm` una única validació de `request_params` consumible des d’app. La mateixa font de veritat governarà la defensa del body final.
4. Integrar-la a `validateModelConfigs` perquè comprovi tots els perfils abans de seleccionar el perfil actiu o aplicar overrides.
5. Conservar l’escriptura dels camps autoritatius al final de la fusió com a defensa en profunditat, encara que una crida interna construeixi opcions sense passar per `parseArgs`.
6. Afegir regressió que una clau TOML desconeguda fora de `request_params` continua fallant amb ruta completa i que una clau arbitrària dins de la taula arriba al proveïdor.
7. Comprovar que errors 4xx per rangs o claus pròpies del proveïdor continuen visibles i no es reintenten.

### Verificació proporcional

- RED/GREEN focal a `internal/llm` per la matriu de claus i valors.
- Integració a `internal/app` amb un transport sentinella que falla el test si rep HTTP davant d’un perfil invàlid.
- Proves de projecció que confirmen que UI, executor i trace no adquireixen `RequestParams`.
- Revisió del diff per garantir que la llista protegida no està duplicada fora d’`internal/llm`.

Comanda de tancament del slice:

```text
env GOCACHE=/tmp/go-build go test -count=1 ./internal/config ./internal/app ./internal/llm ./internal/executor ./internal/trace ./internal/ui
```

**Done evidence:** cada perfil invàlid falla en arrencar amb perfil/ruta, cap HTTP és emès, els camps autoritatius no es poden sobreescriure i els consumidors aliens continuen sense accés als valors.

## 8. Slice 3 — Selecció, migració i contracte públic

### Resultat avançat

Entrega AC4 i AC5: els paràmetres segueixen el perfil seleccionat durant tota la sessió, la migració de `temperature` és explícita i la documentació reflecteix el comportament i els límits reals.

### Owner i límit afectat

- `internal/app` i `internal/config`: selecció, `/model`, precedència i persistència;
- `internal/llm`: propagació d’errors sense canvis de política;
- README, plantilla i web: contracte públic i migració.

### Dependència

Slices 1 i 2 verds; el comportament documentat ha d’estar implementat i protegit.

### Treball executable

1. Afegir proves RED que `/model` canvia els paràmetres efectius de forma atòmica i que la petició següent no conserva cap valor del perfil anterior.
2. Ampliar la caracterització de precedència: `--base-url`, `--model` i `--api-key` modifiquen només els seus camps i conserven els `request_params` del perfil seleccionat; `--model-name` i `SHELLIA_MODEL_NAME` seleccionen el mapa del perfil corresponent.
3. Verificar que persistir `default_model` conserva textualment totes les taules `[models.request_params]` i els permisos/symlinks gestionats pel codi existent.
4. Mantenir la plantilla de Luna sense `temperature` actiu. Si s’hi afegeix descobribilitat, l’exemple serà comentat i no canviarà el default del proveïdor.
5. Actualitzar README i web perquè expliquin:
   - que la taula representa camps del body JSON;
   - tipus acceptats, claus protegides i absència de `null`;
   - semàntica “absent = default del proveïdor”;
   - interacció amb overrides i `/model`;
   - possibles efectes de cost, latència i retenció;
   - rebuig de la taula per binaris Shellia antics.
6. Corregir la secció de traces: es registren prompts, respostes i decisions, però no credencials ni valors arbitraris de `request_params`.
7. Fer una revisió independent focalitzada en autoritat, privacitat, precedència i coherència entre documentació i body capturat.

### Verificació proporcional

- RED/GREEN d’app per canvi interactiu i precedència.
- Regressió de config per persistència atòmica, permisos i symlink.
- Captura HTTP final després de cada canvi de perfil.
- Inspecció de README, plantilla i bloc públic del site contra l’especificació, inclosos links i format.
- Smoke manual opcional amb Luna quan hi hagi credencials; no substitueix la prova automatitzada d’omissió.

Comanda de tancament del slice:

```text
env GOCACHE=/tmp/go-build go test -count=1 ./internal/config ./internal/app ./internal/llm
```

**Done evidence:** `/model` i la selecció inicial produeixen els bodies esperats, la persistència no altera les taules i els tres punts de documentació descriuen la mateixa semàntica.

## 9. Checkpoints agrupats

### Checkpoint 1 — Walking skeleton funcional

Després del Slice 1, una configuració real travessa TOML→perfil→opcions LLM→body capturat, i l’absència de `temperature` queda provada. Encara no és un punt de release fins que el Checkpoint 2 tanqui la frontera.

### Checkpoint 2 — Frontera publicable

Després del Slice 2, configuració arbitrària no pot canviar camps autoritatius ni protocols incompatibles, i tots els perfils fallen aviat. Aquest és el primer estat funcionalment segur.

### Checkpoint 3 — Fase completa

Després del Slice 3, selecció, migració, persistència, diagnòstic i documentació coincideixen amb el body real. No hi ha cap checkpoint addicional per features diferides.

## 10. Seqüència final de verificació

Executar una sola vegada, en aquest ordre:

1. Formatar només els fitxers Go afectats amb `gofmt`.
2. Executar les proves focalitzades:

   ```text
   env GOCACHE=/tmp/go-build go test -count=1 ./internal/config ./internal/app ./internal/llm
   ```

3. Executar la suite completa:

   ```text
   env GOCACHE=/tmp/go-build go test -count=1 ./...
   ```

4. Executar anàlisi estàtica:

   ```text
   env GOCACHE=/tmp/go-build go vet ./...
   ```

5. Compilar el binari fora del repositori:

   ```text
   env GOCACHE=/tmp/go-build go build -o /tmp/shellia-request-params ./cmd/shellia
   ```

6. Executar `git diff --check` i confirmar que el diff no conté canvis aliens ni valors sensibles.
7. Revisar el Completion Proof de §9.4 de l’especificació amb el servidor fals: omissió, passthrough, canvi de perfil i fallada abans d’HTTP.
8. Fer la revisió independent de claus protegides, privacitat i precedència.
9. Si hi ha credencials disponibles, executar el smoke manual no destructiu amb Luna i registrar només l’èxit o l’error, mai la clau ni el body complet.

No s’executa `-race` com a porta obligatòria perquè la fase no introdueix concurrència ni mutació compartida; les proves d’immutabilitat i bytes idèntics cobreixen el risc nou del mapa.

## 11. Stop Gates i handoff

No hi ha decisions materials obertes. Aturar la implementació i tornar a disseny només si:

- BurntSushi TOML no pot conservar algun tipus aprovat sense coerció no documentada;
- la llista protegida necessita permetre un camp que modifica transport, autoritat o forma de resposta;
- els valors han d’entrar en traces o en un consumidor diferent d’LLM per complir l’acceptació;
- conservar `request_params` davant de `--base-url` o `--model` produeix un canvi d’autoritat que exigeix una nova decisió de producte;
- el contracte intern de resposta textual única ha de canviar.

Errors ordinaris de tests, noms interns, ubicació exacta d’un helper o ajustos mecànics de tipus no són Stop Gates; es resolen seguint els propietaris i convencions existents.

Després d’aprovar aquest pla, el handoff és `implement-project-phase`. La implementació seguirà els tres slices en ordre i s’aturarà quan AC1–AC5 i la seqüència final siguin verdes, sense incorporar el treball diferit.
