# Fase de paràmetres de petició per perfil de model

**Data:** 2026-08-11

**Estat:** implementat i verificat

**Nivell:** CRITICAL

**Àmbit:** configuració de perfils, frontera amb proveïdors LLM, body de Chat Completions i migració del `temperature` implícit

## 1. Objectiu, valor i resultat observable

Shellia permetrà declarar camps addicionals del body JSON de Chat Completions dins de cada perfil `[[models]]`, sense haver d'afegir un camp tipat i publicar una versió nova per cada opció específica d'un proveïdor.

El valor immediat és corregir la incompatibilitat del perfil per defecte `gpt-5.6-luna`, que rebutja el `temperature: 0` que Shellia envia avui, i establir alhora una extensió acotada per opcions com `temperature`, `top_p`, límits de tokens o controls de raonament.

El resultat observable serà:

- un perfil sense `request_params` no enviarà `temperature` ni cap altre valor addicional, de manera que el proveïdor aplicarà els seus defaults;
- cada perfil podrà declarar el seu propi `[models.request_params]`;
- canviar de perfil amb `/model` canviarà també els paràmetres efectius;
- claus incompatibles amb el contracte de Shellia o valors que no es puguin representar com a JSON fallaran en arrencar, abans de fer cap petició;
- els errors semàntics de claus o rangs específics continuaran sent responsabilitat del proveïdor i es mostraran mitjançant el tractament HTTP actual.

Exemple:

```toml
[[models]]
name = "openai-luna"
base_url = "https://api.openai.com/v1"
model = "gpt-5.6-luna"
api_key_env = "SHELLIA_API_KEY"
supports_response_format = true

[models.request_params]
temperature = 1
reasoning_effort = "medium"
```

`request_params` significa camps addicionals del body JSON, no query parameters de la URL.

## 2. Capacitat actual i prerequisits

Shellia ja disposa de les fronteres necessàries:

- `internal/config` carrega `[[models]]`, normalitza els perfils i rebutja claus TOML desconegudes amb la ruta completa;
- `internal/app` valida tots els perfils, en selecciona un segons la precedència existent i aplica el perfil actiu;
- `/model` reutilitza l'aplicació del perfil i persisteix només `default_model`, preservant la resta del fitxer;
- `llmClientOptions` projecta una vista estreta de configuració cap a `internal/llm`;
- `internal/llm` és propietari del contracte Chat Completions, construeix `model`, `messages` i `response_format`, serialitza una vegada el body i conserva el mateix body durant els reintents;
- els errors HTTP 4xx ja es propaguen sense reintentar-los i les respostes 429/5xx mantenen la política actual;
- les proves disposen d'un servidor HTTP fals que captura els bodies de petició.

El prerequisit funcional ja està aprovat: quan un paràmetre no es configura, Shellia no l'envia i el valor per defecte queda determinat pel proveïdor.

La fase reutilitza `map[string]any`, el descodificador TOML i `encoding/json` existents. No requereix dependències, un registre de proveïdors ni un sistema nou d'esquemes.

## 3. Abast

### 3.1 Dins de l'abast

- Afegir un `[models.request_params]` opcional a cada `[[models]]`.
- Transportar els paràmetres del perfil seleccionat per la frontera estreta `config → app → llm`.
- Ometre completament els paràmetres absents.
- Eliminar el `temperature: 0` implícit de totes les peticions de planificació.
- Acceptar valors TOML que es puguin representar de manera inequívoca en JSON:
  - strings;
  - booleans;
  - enters;
  - floats finits;
  - arrays dels mateixos tipus;
  - taules niades amb claus string.
- Validar recursivament tots els perfils configurats, encara que no estiguin actius.
- Rebutjar claus protegides que Shellia necessita controlar.
- Mantenir la selecció de perfils, la precedència, els reintents, els límits de resposta i el tractament d'errors actuals.
- Documentar el significat, la precedència, les limitacions, els efectes de cost/privacitat i la compatibilitat amb versions anteriors.

### 3.2 Fora de l'abast

- Paràmetres globals o herència entre perfils.
- Overrides de `request_params` per flags, variables d'entorn, slash commands o per torn.
- Detecció automàtica del proveïdor, esquemes específics, autocomplete o validació de rangs de cada model.
- Capçaleres HTTP, query parameters, endpoints o credencials addicionals configurables.
- Interpolació de variables d'entorn o secrets dins de `request_params`.
- JSON `null`, perquè TOML no té una representació nativa equivalent.
- Streaming, múltiples choices, tool calls, respostes d'àudio o altres modalitats que canviïn el contracte de resposta textual únic.
- Migració a Responses API o suport per APIs que no siguin compatibles amb Chat Completions.
- Límits nous de mida, profunditat o longitud específics per `request_params`; la configuració continua sent local i de confiança i es conserven els límits HTTP existents.
- Un sandbox genèric per efectes remots iniciats pel proveïdor.

## 4. Arquitectura i autoritat

### 4.1 Flux de dades

```text
FileModelConfig.RequestParams
  → ModelConfig.RequestParams
  → Config.RequestParams del perfil actiu
  → llm.ClientOptions.RequestParams
  → body JSON nou per petició
```

El mapa es tracta com a immutable després de carregar i normalitzar la configuració. La construcció de cada petició crea un objecte JSON nou; no modifica el perfil ni reutilitza el mapa com a body mutable.

### 4.2 Propietaris

- `internal/config` és propietari de la representació TOML i de la normalització dels valors.
- `internal/app` és propietari de validar tots els perfils en arrencar, seleccionar-ne un i projectar-lo als consumidors.
- `internal/llm` és l'únic propietari de les claus protegides, de la validació del contracte del body i de la fusió final.
- el proveïdor és propietari de l'existència, el tipus semàntic, els rangs i la combinació vàlida de les claus no protegides.
- l'usuari que edita el fitxer local és l'autoritat que accepta els efectes de cost, latència, retenció o comportament del proveïdor associats als valors configurats.

Per evitar duplicar el contracte HTTP, `internal/llm` exposarà la validació mínima que `internal/app` necessita per comprovar tots els perfils durant l'arrencada. La fusió es defensarà igualment en el punt de serialització: Shellia escriurà els seus camps autoritatius al final, fins i tot després d'haver rebut una configuració validada.

### 4.3 Claus protegides

La primera versió rebutjarà coincidències exactes i sensibles a majúscules en el nivell superior de `request_params` per a:

```text
model
messages
response_format
stream
stream_options
n
tools
tool_choice
parallel_tool_calls
functions
function_call
modalities
audio
web_search_options
```

La política és:

- `model`, `messages` i `response_format` són camps autoritatius de Shellia;
- `stream`, `stream_options` i `n` trenquen el transport o la cardinalitat que el client actual sap consumir;
- tools, functions i cerca gestionada pel proveïdor poden produir accions o respostes sense `message.content` textual;
- `modalities` i `audio` canvien el tipus de resposta.

Una clau amb el mateix nom dins d'una taula niada no col·lideix amb el nivell superior. Futures claus que canviïn el transport, l'autoritat o la forma de resposta requeriran suport explícit i s'afegiran al conjunt protegit com un canvi documentat de compatibilitat.

`supports_response_format` continuarà sent un camp tipat del perfil perquè Shellia necessita entendre aquesta capacitat; no es trasllada a `request_params`.

### 4.4 Validació de valors

La validació recorrerà arrays i taules niades i fallarà en arrencar davant de:

- dates o hores TOML, que altrament es convertirien silenciosament en strings JSON;
- floats `nan`, `+inf` o `-inf`;
- qualsevol valor que `encoding/json` no pugui representar;
- qualsevol clau protegida al nivell superior.

No s'intentarà validar si, per exemple, un proveïdor accepta `temperature = 1.7` o `reasoning_effort = "high"`. Un 4xx del proveïdor conservarà el cos diagnòstic actual.

Els errors locals no inclouran valors i identificaran com a mínim el perfil i la ruta afectada, per exemple:

```text
configured model profile "openai": request_params.model is reserved by Shellia
configured model profile "local": request_params.sampling[1] is not JSON-compatible
```

## 5. Precedència, canvi de perfil i migració

### 5.1 Precedència

`request_params` forma part del perfil seleccionat i no té cap capa pròpia d'override. La precedència general es manté:

1. defaults interns;
2. perfil `[[models]]` seleccionat;
3. variables d'entorn existents;
4. flags existents.

Els overrides actuals de `--base-url`, `--model` i `--api-key` modifiquen aquests camps però no esborren `request_params`. Per canviar de proveïdor amb un conjunt coherent de paràmetres, l'usuari haurà de seleccionar el perfil corresponent amb `--model-name` o `SHELLIA_MODEL_NAME`. Aquesta interacció s'explicarà a la documentació.

### 5.2 Canvi interactiu

`/model <name>` aplicarà de manera conjunta endpoint, model, credencials, capacitats i `request_params` del perfil. La persistència continuarà canviant només `default_model`; el body complet de cada perfil es conservarà.

### 5.3 Migració

No hi haurà reescriptura automàtica dels fitxers existents.

- Els fitxers antics continuaran carregant-se.
- Un perfil antic sense `request_params` deixarà d'enviar el `temperature: 0` implícit i passarà a utilitzar el default del proveïdor.
- El perfil nou `gpt-5.6-luna` funcionarà sense declarar `temperature`, perquè Shellia l'ometrà.
- La plantilla generada no fixarà `temperature = 1`; pot mostrar un exemple comentat, però conservarà la semàntica “absent significa default del proveïdor”.
- Una configuració que utilitzi `[models.request_params]` serà rebutjada per versions antigues de Shellia, perquè el parser antic tracta la taula com una clau desconeguda. La documentació ho indicarà com a requisit de versió.

El canvi de `temperature: 0` a camp omès és una migració de comportament intencionada i aprovada. Pot variar la determinació de models existents; qui necessiti conservar un valor explícit l'haurà de declarar al perfil corresponent.

## 6. Confiança, privacitat i operació

`request_params` és configuració local de confiança, però tot el seu contingut s'envia al proveïdor seleccionat. La documentació advertirà que alguns camps poden modificar cost, latència, retenció, caching o efectes remots del proveïdor.

Shellia no registrarà els valors de `request_params` a UI, executor, metadades de sessió ni traces. Això preserva el límit actual que evita propagar credencials i impedeix que claus arbitràries amb dades sensibles quedin persistides. Els errors locals mostraran rutes, no valors; els errors HTTP conservaran el tractament i truncament actuals.

La documentació de traces deixarà de prometre implícitament el body HTTP complet: especificarà que les traces contenen prompts, respostes, decisions i execucions, però no credencials ni paràmetres arbitraris del proveïdor.

Els reintents continuaran reutilitzant exactament el mateix body serialitzat. No es reavaluarà ni mutarà `request_params` entre intents.

## 7. Recorregut d'acceptació i condicions de fallada

### 7.1 Recorregut mínim executable

1. L'usuari crea dos perfils: un sense `request_params` i un altre amb valors escalars i niats.
2. Shellia carrega i valida tots dos perfils abans de seleccionar-ne un.
3. Una petició amb el primer perfil conté `model`, `messages` i el `response_format` aplicable, però no conté `temperature` ni cap camp addicional.
4. Una petició amb el segon perfil conté els camps addicionals amb la mateixa forma JSON configurada.
5. `/model` canvia al segon perfil i la petició següent utilitza els seus paràmetres, sense conservar els del primer.
6. Un error semàntic retornat pel proveïdor es mostra com ara i no provoca reintents de 4xx.

### 7.2 Condicions de fallada obligatòries

Shellia ha de fallar abans de fer cap petició quan qualsevol perfil contingui:

- una clau protegida;
- una data o hora TOML;
- un float no finit;
- un valor no serialitzable a JSON.

També és fallada de fase si:

- un perfil hereta paràmetres d'un altre;
- un paràmetre absent apareix al body amb zero, string buit o `null`;
- canviar amb `/model` conserva el mapa anterior;
- la fusió permet sobreescriure un camp autoritatiu;
- els valors apareixen en traces, UI o opcions d'executor;
- una clau desconeguda fora de `request_params` deixa de ser rebutjada;
- la configuració per defecte de Luna continua enviant `temperature: 0`.

## 8. Capacitats, dependències i checkpoints

### Capacitat A: extensió de configuració per perfil

Permet carregar i conservar un mapa opcional per cada perfil sense relaxar la validació estricta fora de `[models.request_params]`.

No depèn de cap altra capacitat de la fase i crea el contracte de dades mínim.

### Capacitat B: validació primerenca del límit de confiança

Valida tots els perfils, el domini TOML→JSON i les claus protegides amb errors accionables.

Depèn de la capacitat A i del contracte de claus propietat d'`internal/llm`.

### Capacitat C: lliurament determinista al proveïdor

Projecta els paràmetres del perfil actiu, crea un body nou, hi incorpora els camps addicionals i reafirma al final els camps propietat de Shellia. L'absència produeix omissió real.

Depèn de les capacitats A i B.

### Capacitat D: coherència de selecció, migració i diagnòstic

Fa que `/model`, les regles de precedència, la documentació, les traces i la plantilla expliquin i preservin el mateix comportament.

Depèn de la capacitat C perquè documenta i prova el resultat efectiu.

### Checkpoint 1: walking skeleton de perfil a body

- Un perfil amb `temperature` i una taula niada arriba amb forma exacta a un servidor HTTP fals.
- Un perfil sense paràmetres no envia `temperature`.
- Els camps `model`, `messages` i `response_format` continuen correctes.

Aquest checkpoint travessa la frontera nova més aviat i prova el valor visible abans d'afegir tot l'enduriment.

### Checkpoint 2: frontera segura i canvi de perfil

- Tots els perfils es validen en arrencar.
- Les claus protegides i els valors incompatibles fallen amb perfil i ruta.
- `/model` canvia el conjunt efectiu complet.
- La configuració estricta fora de la taula es manté.
- Cap consumidor aliè a LLM rep el mapa.

### Checkpoint 3: migració i contracte públic

- S'elimina el `temperature: 0` implícit.
- La plantilla, README i web mostren la mateixa semàntica.
- Es documenten els riscos de cost/privacitat, la interacció amb overrides i la incompatibilitat amb binaris antics.
- La política de traces queda alineada amb el comportament real.

## 9. Verificació proporcional

Com que la fase modifica una frontera de proveïdor, la verificació serà CRITICAL però focalitzada.

### 9.1 TDD focal

Les proves de comportament precediran cada canvi crític:

- omissió de `temperature` quan no està configurat;
- serialització exacta de strings, booleans, enters, floats, arrays i taules niades;
- rebuig de cada categoria de clau protegida;
- rebuig recursiu de dates i floats no finits;
- validació de perfils inactius;
- canvi de paràmetres mitjançant `/model`;
- impossibilitat de sobreescriure camps de Shellia;
- absència de regressions en `response_format` i propagació de 4xx.

### 9.2 Integració i regressió

- Proves del parser confirmen que les claus arbitràries només són acceptades dins de `request_params`.
- Proves de projeccions confirmen que només LLM rep els paràmetres.
- El servidor HTTP fals captura el body final, inclosos reintents idèntics.
- Les proves de persistència confirmen que `/model` conserva les taules dels perfils.
- La suite completa passa amb `env GOCACHE=/tmp/go-build go test -count=1 ./...`.
- La compilació del binari passa.

### 9.3 Revisió independent

Una revisió separada comprovarà específicament:

- que el conjunt de claus protegides cobreix tots els camps que canvien el transport o la forma de resposta actual;
- que no hi ha duplicació del conjunt entre config, app i llm;
- que els valors no arriben a traces, UI ni executor;
- que la precedència i la migració estan documentades sense prometre compatibilitat universal amb qualsevol API.

### 9.4 Completion Proof mínim

Una única seqüència automatitzada demostrarà:

1. un perfil “provider-default” produeix un body sense `temperature`;
2. un perfil “custom” produeix el body esperat amb paràmetres escalars i niats;
3. `/model custom` fa que la petició següent utilitzi només els paràmetres de “custom”;
4. afegir `request_params.model` a qualsevol perfil fa fallar l'arrencada abans de contactar el servidor;
5. la suite completa i el build passen.

Quan hi hagi credencials disponibles, un smoke manual no destructiu amb `gpt-5.6-luna` confirmarà que una consulta simple ja no rep el 400 de `temperature`. Aquest smoke no substituirà la prova determinista del body ni serà obligatori a CI.

## 10. Stop Gates i treball diferit

### Stop Gates

No queden decisions materials obertes per redactar un pla d'execució. Han quedat fixades:

- ubicació per perfil;
- semàntica d'omissió i default del proveïdor;
- mapa únic per a tots els camps consumits pel proveïdor, inclòs `temperature`;
- fallada en arrencar davant de claus protegides;
- validació de forma local i validació semàntica del proveïdor;
- absència de cap capa global, CLI o entorn en aquest increment;
- exclusió dels valors de traces i consumidors no LLM.

La implementació s'aturarà i tornarà a disseny només si es descobreix que una clau protegida és necessària per complir el recorregut aprovat, que el parser TOML no pot conservar la forma definida sense una representació nova, o que la fusió exigeix canviar el contracte de resposta textual actual.

### Treball diferit

- Suport explícit de `null`.
- Esquemes, autocomplete i validació específica de proveïdors.
- Paràmetres globals, herència o overrides per torn.
- Headers, query parameters, endpoints alternatius o Responses API.
- Streaming, múltiples choices, tools, cerca gestionada pel proveïdor, àudio i multimodalitat.
- Interpolació segura de secrets.
- Redacció configurable o traça opt-in dels paràmetres efectius.
- Límits específics de mida o profunditat si la configuració deixa de ser exclusivament local i de confiança.

## 11. Handoff

La fase conté tres checkpoints ordenats i creua configuració, aplicació, client LLM, migració i documentació. Després d'aprovar aquesta especificació, el següent pas és `design-project-plan` per convertir els checkpoints en slices executables i verificables. La implementació no començarà des d'aquest document sense aquest pla separat.
