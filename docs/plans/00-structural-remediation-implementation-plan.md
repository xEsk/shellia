# Pla 00 — Programa de correcció estructural de Shellia

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan phase-by-phase, and use `implement-project-phase` as the execution workflow. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Corregir els vuit punts de l’anàlisi estructural sense reescriure Shellia ni alterar comportaments no relacionats.

**Architecture:** El treball avança en vuit fases estrictament ordenades. Seguretat i propietat del procés es corregeixen abans d’establir CI; després s’estrenyen identitat i dependències, i només al final es descomponen l’orquestració i el contracte LLM.

**Tech Stack:** Go 1.26, biblioteca estàndard, GitHub Actions, `golangci-lint`, proves `testing` existents i `runtimeDeps`.

**Especificació canònica:** [`docs/superpowers/specs/2026-08-09-structural-remediation-program-design.md`](../superpowers/specs/2026-08-09-structural-remediation-program-design.md)

## Global Constraints

- Executar les fases en ordre: `1 → 2 → 3 → 4 → 5 → 6 → 7 → 8`.
- Preservar configuració, flags, sortides, codis de retorn, traces i comportaments existents excepte les correccions explícites.
- Reutilitzar `runtimeDeps`, els paquets actuals i les proves de caracterització.
- No afegir frameworks, parsers de shell, contenidors de dependències ni paquets arquitectònics.
- No crear tags, releases, publicacions ni canvis al web públic.
- Aturar-se davant un canvi incompatible, una dependència nova, un conflicte amb canvis de l’usuari o una regressió no explicada.
- Cada fase ha d’acabar en un canvi funcional independent i un commit focalitzat després de superar la seva porta de fase.

---

## Resultat i criteris enllaçats

El pla cobreix, en el mateix ordre que l’especificació: substitucions de shell, `os.Exit` profund, CI/lint, ruta del mòdul, àlies, propagació de configuració, orquestració i contracte LLM. Cada fase referencia directament els seus criteris d’acceptació de l’especificació i s’atura tan aviat com aquests són demostrables.

## Nivell i proporcionalitat

Nivell `CRITICAL`: les fases 1, 2, 7 i 8 modifiquen autoritat d’execució, finalització del procés o contractes externs. Aquestes fases exigeixen RED/GREEN, proves d’integració i comprovació de regressions. Les fases 3, 4 i 5 són principalment mecàniques; les fases 6 i 7 són canvis de frontera i requereixen proves dels dos costats de cada contracte.

## Abast

### Inclòs

- Els vuit punts de l’especificació aprovada.
- Els canvis mínims de documentació necessaris per CI i instal·lació Go.
- Nous fitxers dins de paquets existents quan separen responsabilitats ja aprovades.
- Proves de regressió, caracterització i contracte necessàries per demostrar l’acceptació.

### Exclòs

- Funcionalitats noves, redissenys visuals i canvis de proveïdor LLM.
- Canvis de format de `config.toml` o de precedència de configuració.
- Optimitzacions no mesurades i refactoritzacions sense criteri d’acceptació associat.
- Reorganitzar `internal/core` o crear una capa comuna nova.

### Diferit

- Activar linters sorollosos que `.golangci.yml` manté explícitament desactivats.
- Fer més estricte l’esquema JSON respecte de camps desconeguts.
- Canviar el contingut o l’estratègia del prompt després de l’extracció mecànica.
- Qualsevol divisió addicional de fitxers que no sigui necessària per completar les fases 7 i 8.

## Propietaris i punts de reutilització

- `internal/safety`: `classifyCommand`, `commandRoots`, `hasDangerousCommandRoot`, `hasShellOperators` i les regles locals.
- `internal/app`: `runApp`, `runInteractive`, `runTurn`, `runPlanningRound`, `workflowState` i `runtimeDeps`.
- `internal/executor`: `ExecuteCommands`, `ExecuteManualCommand`, `RuntimeDeps`, captura, cancel·lació i revalidació després d’editar.
- `internal/ui`: `Renderer`, `Turn`, `StepBox`, confirmacions i renderers visuals existents.
- `internal/config`: `Config`, `ModelConfig`, defaults, càrrega i persistència.
- `internal/llm`: transport, `PromptRequest`, `ParseResponse`, normalització i construcció de prompts.
- `internal/session`: actualització de memòria de seguiment.
- `internal/trace`: obertura de sessió, esdeveniments i tancament.
- `.github/workflows/release.yml` i `pages.yml`: workflows que s’han de conservar.

## Porta comuna de cada fase

Després de les proves focalitzades de cada fase:

- [ ] Executar `env GOCACHE=/tmp/go-build go test -count=1 ./...` i obtenir PASS.
- [ ] Executar `env GOCACHE=/tmp/go-build go vet ./...` i obtenir sortida buida.
- [ ] Executar `env GOCACHE=/tmp/go-build GOLANGCI_LINT_CACHE=/tmp/shellia-golangci-lint golangci-lint run ./...` i obtenir sortida buida a partir de la fase 3.
- [ ] Executar `env GOCACHE=/tmp/go-build go build -o /tmp/shellia-plan-00 ./cmd/shellia` i obtenir el binari.
- [ ] Executar `git diff --check` i revisar que el diff només conté l’abast de la fase.
- [ ] Quan s’afectin bucles, traces, executor o UI compartida, executar `env GOCACHE=/tmp/go-build go test -race -count=1 ./internal/app ./internal/executor ./internal/trace ./internal/ui`.
- [ ] Registrar la fase com a completada només després de crear-ne el commit focalitzat.

La fase 1 pot conservar temporalment les 26 incidències de lint de la línia base. La fase 2 no n’ha d’afegir cap. La fase 3 elimina aquesta excepció definitivament.

## Fase 1 — Classificador de seguretat

**Resultat avançat:** cap substitució executable pot ser `LocalSafe`, i les substitucions amb arrels perilloses són d’alt risc.

**Propietari i frontera:** `internal/safety`; integració de confirmació a `internal/executor`.

**Dependències:** cap. Aquesta fase no canvia signatures públiques.

**Fitxers:**

- Modify: `internal/safety/safety.go`
- Modify: `internal/safety/safety_test.go`
- Modify: `internal/executor/executor_test.go`

- [ ] Afegir casos tabulars RED per `$()` i accents greus fora de cometes i dins de cometes dobles; incloure `touch`, `rm`, substitucions niades, escapament, cometes simples i literals com `echo "a > b"`.
- [ ] Afegir `TestExecuteCommandsDoesNotAutoRunCommandSubstitutionWithYesSafe`: amb `YesSafe=true`, una ordre amb substitució ha de demanar confirmació i no arribar al runner si l’usuari la rebutja.
- [ ] Executar `env GOCACHE=/tmp/go-build go test -count=1 ./internal/safety ./internal/executor` i confirmar que els casos nous fallen pel bypass actual.
- [ ] Centralitzar l’escaneig mínim de sintaxi perquè `hasShellOperators` i `commandRoots` comparteixin estat de cometes, escapaments i context de substitució. Conservar aquests dos helpers com a façana interna i no interpretar el shell complet.
- [ ] Fer que l’escàner detecti `$(` i accents greus dins de cometes dobles, ignori el contingut literal de cometes simples i exposi les arrels executables de les substitucions a `hasDangerousCommandRoot`.
- [ ] Executar les proves focalitzades i confirmar GREEN, inclòs `echo "$(touch /tmp/shellia-demo)"` com a no segur i la variant amb `rm` com a perillosa.
- [ ] Superar la porta comuna de fase, aplicant l’excepció temporal de lint documentada.

**Done evidence:** proves RED/GREEN de safety i executor; cap ordre de substitució evita la confirmació amb `yes_safe`.

## Fase 2 — Propietat dels errors i del procés

**Resultat avançat:** només `cmd/shellia/main.go` finalitza el procés i tots els errors interactius passen per `runApp`.

**Propietari i frontera:** `internal/app` controla codis de retorn; UI només presenta errors.

**Dependències:** fase 1 completada.

**Fitxers:**

- Modify: `internal/app/runtime.go`
- Modify: `internal/app/main.go`
- Modify: `internal/app/main_loop_test.go`
- Modify: `internal/app/aliases.go`
- Modify: `internal/ui/ui.go`
- Modify: `internal/ui/export.go`

- [ ] Afegir a `runtimeDeps` una funció injectada de lectura amb la mateixa signatura que `ui.ReadInteractivePromptWithRenderer`; el default de producció apunta a aquesta funció i les proves poden retornar un error no EOF sense tocar globals.
- [ ] Afegir `TestRunAppInteractiveReadErrorReturnsOneAndClosesTrace`: ha de comprovar codi `1`, missatge a `deps.Stderr`, un únic `session_end` i tancament correcte del logger.
- [ ] Executar el test nou i confirmar RED perquè `exitWithError` mata el procés en lloc de retornar.
- [ ] Canviar `runInteractive` perquè retorni `error`; EOF i cancel·lació normal retornen `nil`, i un error de lectura retorna `fmt.Errorf("cannot read prompt: %w", err)`.
- [ ] Fer que `runApp` imprimeixi aquest error amb `deps.Stderr`, retorni `1` i deixi executar els `defer` de trace.
- [ ] Eliminar `exitWithError`, `ui.ExitWithError`, l’àlies d’app i la importació d’`os` de UI si ja no té cap altre ús.
- [ ] Executar `rg -n "os\\.Exit" cmd internal` i confirmar que l’únic ús de producció és `cmd/shellia/main.go`.
- [ ] Executar `env GOCACHE=/tmp/go-build go test -count=1 ./internal/app ./internal/ui ./internal/trace` i confirmar GREEN per error, EOF i cancel·lació.
- [ ] Superar la porta comuna de fase, inclosa la comprovació `-race`.

**Done evidence:** el procés de proves sobreviu a l’error injectat, `runApp` retorna `1` i el trace finalitza.

## Fase 3 — CI i línia base de qualitat

**Resultat avançat:** el lint local és verd i push/pull request executen build, tests, vet, lint i race.

**Propietari i frontera:** `.golangci.yml`, Go afectat per les 26 incidències i `.github/workflows`.

**Dependències:** fases 1 i 2 completades.

**Fitxers:**

- Create: `.github/workflows/ci.yml`
- Modify (`gofumpt`): `cmd/shellia/main.go`, `internal/app/main_loop_test.go`, `internal/config/config.go`, `internal/config/export.go`, `internal/executor/export.go`, `internal/executor/test_helpers_test.go`, `internal/llm/export.go`, `internal/session/session_memory.go`, `internal/ui/visual_bands.go`, `internal/ui/visual_bands_test.go`, `internal/ui/visual_cards.go`, `internal/ui/visual_cards_test.go`, `internal/ui/visual_guide.go`, `internal/ui/visual_guide_test.go`, `internal/ui/visual_plain.go`, `internal/ui/visual_plain_test.go`, `internal/ui/visual_renderer.go` i `internal/ui/visual_renderer_test.go`
- Modify: `internal/app/main.go`
- Modify: `internal/app/main_loop_test.go`
- Modify: `internal/ui/visual_guide_test.go`
- Modify: `internal/ui/ui_test.go`
- Modify: `internal/session/session_memory.go`
- Review only: `.github/workflows/release.yml`, `.github/workflows/pages.yml`, `.golangci.yml`

- [ ] Capturar la línia base amb `env GOCACHE=/tmp/go-build GOLANGCI_LINT_CACHE=/tmp/shellia-golangci-lint golangci-lint run ./...` i conservar la classificació: 18 `gofumpt`, 2 `errorlint`, 2 `dupword`, 1 `forcetypeassert`, 1 `intrange` i 2 `whitespace`.
- [ ] Aplicar `gofumpt` només als 18 fitxers reportats i revisar que els canvis siguin mecànics.
- [ ] Corregir els dos `fmt.Errorf("%w: %v", ...)` de `runPlanningRound` perquè preservin `errStructuralResponse` i la causa original amb wrapping múltiple.
- [ ] Documentar amb `nolint:dupword` específic els inputs repetits `y` dels dos tests, comprovant que cada resposta correspon a una confirmació diferent.
- [ ] Fer segura l’asserció de tipus de `visual_guide_test.go`, convertir el bucle de dues iteracions d’`ui_test.go` a integer range i eliminar els dos espais finals reportats.
- [ ] Executar el lint fins a obtenir zero incidències sense desactivar cap linter addicional.
- [ ] Crear `ci.yml` per a `push` i `pull_request`, amb checkout, Go des de `go.mod`, build, suite completa, `go vet`, `golangci/golangci-lint-action@v8` i race als paquets `app`, `executor`, `trace` i `ui`.
- [ ] Verificar que `release.yml` continua limitat a tags `v*` i `pages.yml` a canvis del site; no duplicar release ni desplegament a `ci.yml`.
- [ ] Superar la porta comuna sense cap excepció de lint.

**Done evidence:** lint local verd, `ci.yml` sintàcticament revisat i primera execució remota verda quan el commit arribi a GitHub.

## Fase 4 — Identitat canònica del mòdul

**Resultat avançat:** el projecte es resol com `github.com/xEsk/shellia` i documenta instal·lació remota estàndard.

**Propietari i frontera:** `go.mod`, importacions Go, GoReleaser i README.

**Dependències:** CI verda de la fase 3.

**Fitxers:**

- Modify: `go.mod`
- Modify: tots els `.go` que importen `shellia/internal/...`
- Modify: `README.md`
- Review: `.goreleaser.yaml`

- [ ] Canviar la declaració de mòdul a `github.com/xEsk/shellia` i substituir mecànicament totes les importacions `shellia/internal/...`.
- [ ] Executar `gofmt` sobre els fitxers amb importacions modificades i `go mod tidy`; `go.sum` només canvia si Go ho necessita realment.
- [ ] Afegir a Installation l’ordre `go install github.com/xEsk/shellia/cmd/shellia@latest`, mantenint download i build local.
- [ ] Revisar que `.goreleaser.yaml` conserva `main: ./cmd/shellia` i `-X main.version={{.Version}}`; no crear release.
- [ ] Executar `go list ./...` i confirmar que totes les rutes comencen per `github.com/xEsk/shellia`.
- [ ] Executar `rg -n '"shellia/internal/' --glob '*.go'` i confirmar sortida buida.
- [ ] Superar la porta comuna de fase.

**Done evidence:** `go list`, tests i build resolen la ruta pública; README conté el `go install` canònic.

## Fase 5 — Àlies i dependències ocultes

**Resultat avançat:** les crides mostren el paquet propietari i UI ja no depèn de LLM.

**Propietari i frontera:** `aliases.go` de cada paquet i els seus punts d’ús.

**Dependències:** importacions canòniques de la fase 4.

**Fitxers:**

- Modify: `internal/app/aliases.go`, `main.go`, `runtime.go`, `presentation.go`
- Modify: `internal/executor/aliases.go`, `executor.go`, `writers.go`
- Modify: `internal/llm/aliases.go`, `llm.go`
- Modify: `internal/ui/aliases.go`, `ui.go`
- Modify: `internal/trace/aliases.go`, `trace.go`
- Review: `internal/session/aliases.go` i tots els tests afectats per noms qualificats

- [ ] Eliminar `llmResponse` i la importació `internal/llm` de `internal/ui/aliases.go`; executar `go list -deps ./internal/ui` i confirmar que LLM no és dependència.
- [ ] Substituir les variables-funció d’`app/aliases.go` per imports i crides qualificades. Conservar injecció només a `runtimeDeps`; la lectura interactiva injectada a la fase 2 no torna a ser global.
- [ ] Substituir les variables-funció equivalents d’executor, LLM i trace per crides qualificades als propietaris `safety`, `session`, `trace`, `config` i `ui`.
- [ ] Eliminar façanes públiques/privades duplicades de tipus i constants quan cap consumidor les necessita; conservar temporalment els àlies de tipus que eviten una migració aliena als criteris de la fase.
- [ ] Revisar cada `aliases.go` restant i verificar que no conté variables mutables que només reexporten una funció.
- [ ] Executar les proves de paquets després de cada propietari: `./internal/ui`, `./internal/llm`, `./internal/trace`, `./internal/executor` i `./internal/app`.
- [ ] Superar la porta comuna de fase.

**Done evidence:** UI no importa LLM; app usa `llm.ParseResponse` i altres propietaris de forma explícita; no queden façanes de funció mutables.

## Fase 6 — Vistes estretes de configuració

**Resultat avançat:** cada paquet rep només les opcions que consumeix i `APIKey` queda confinada al transport LLM.

**Propietari i frontera:** app deriva opcions; cada paquet consumidor defineix el seu contracte mínim.

**Dependències:** propietaris explícits de la fase 5.

**Fitxers:**

- Create: `internal/app/options.go`
- Modify: `internal/app/main.go`, `runtime.go`, `workflow.go` i proves de configuració/orquestració
- Modify: `internal/executor/export.go`, `executor.go`, `writers.go` i proves
- Modify: `internal/ui/export.go`, `ui.go`, `visual_renderer.go`, els quatre `visual_*.go` i proves visuals
- Modify: `internal/llm/export.go`, `llm.go` i proves
- Modify: `internal/session/export.go`, `session_memory.go` i proves
- Modify: `internal/trace/export.go`, `trace.go` i proves

- [ ] Definir a executor `ContextOptions{IncludeUser}` i `Options{CommandTimeout, YesSafe, ContinueOnError, ConfirmationDefault, CaptureStdoutBytes, CaptureStderrBytes, ShowSystemOutput}`; `GetContext`, `ExecuteCommands` i `ExecuteManualCommand` deixen de rebre `config.Config`.
- [ ] Definir a UI `ModelOption{Name, Model}` i `ViewOptions` amb només model actiu/llista pública, flags de presentació, visibilitat de context, estil visual i autoritat `PlanOnly`; no incloure `APIKey`, `BaseURL` ni `APIKeyEnv`.
- [ ] Definir a LLM `ClientOptions{BaseURL, APIKey, Model, RequestTimeout, SupportsResponseFormat}` i `PromptOptions{PlanOnly, IncludeCWD, IncludeOS, IncludeShell, IncludeUser, IncludeSessionMemory, IncludeRecentObservations, MaxObservationEntries, ObservationOutputChars, TruncationStrategy}`. `PromptRequest.Config` passa a ser `PromptOptions`.
- [ ] Fer que `planningRoundRequest` transporti separadament `llm.ClientOptions` i `llm.PromptRequest`: transport i parsing consumeixen `ClientOptions`, mentre que la construcció del prompt consumeix només `PromptOptions`.
- [ ] Definir a session `MemoryOptions{MaxObservationEntries, MemoryObservationChars, TruncationStrategy}` i a trace `Options` amb només camps de trace i metadades no secretes que ja s’escriuen.
- [ ] Implementar a `internal/app/options.go` les conversions unidireccionals des de `config.Config` cap a cada vista; app continua sent l’únic lloc que conserva la configuració completa durant l’orquestració.
- [ ] Adaptar renderer, executor, LLM, session i trace una frontera cada vegada, executant les proves del consumidor abans de passar al següent.
- [ ] Eliminar `HTTPClient` d’`executor.RuntimeDeps` i d’`executorDeps`; el client HTTP es manté exclusivament a `app.runtimeDeps` per a LLM.
- [ ] Executar `rg -n '\b(APIKey|APIKeyEnv)\b' internal/executor internal/ui` i confirmar sortida buida.
- [ ] Afegir proves de conversió a `internal/app/config_test.go` que demostrin els camps necessaris i l’absència de secrets a UI/executor, i mantenir verdes les proves de flags, model, tema i precedència.
- [ ] Superar la porta comuna de fase, inclosa la comprovació `-race`.

**Done evidence:** cap signatura d’executor/UI rep `config.Config`, executor no té HTTP i només `llm.ClientOptions` transporta `APIKey` fora d’app/config.

## Fase 7 — Descomposició de l’orquestració

**Resultat avançat:** una sola ruta aplica resultats de torn, `runTurn` delega responsabilitats i executor no importa UI.

**Propietari i frontera:** app conserva orquestració; executor defineix el contracte que consumeix; app adapta UI.

**Dependències:** vistes estretes de la fase 6.

**Fitxers:**

- Create: `internal/executor/presentation.go`
- Create: `internal/app/executor_presentation.go`
- Create: `internal/app/interactive_loop.go`
- Create: `internal/app/turn.go`
- Modify: `internal/app/main.go`, `runtime.go`, `workflow.go`, `main_loop_test.go`, `workflow_test.go`
- Modify: `internal/executor/export.go`, `executor.go`, `writers.go`, `aliases.go`, `executor_test.go`
- Modify: `internal/ui/export.go`, `visual_renderer.go`, `ui_stepbox.go` i proves visuals

- [ ] Congelar caracterització abans de moure codi: executar els tests `TestRunInteractive*`, `TestRunTurn*`, `TestVisualStylesPreserveOneSemanticTranscript`, `TestExecuteCommands*` i guardar els transcripts esperats existents sense actualitzar-los.
- [ ] Definir a `internal/executor/presentation.go` els contractes mínims `Presenter`, `TurnPresenter` i `StepPresenter`, més decisions i tons semàntics propis d’executor. Només han d’incloure inici/tancament de pas, confirmació/edició, estat, output, avís i suspend/resume utilitzats actualment.
- [ ] Implementar `executorPresenter` a app com adaptador de `ui.Renderer`, `ui.Turn`, `ui.StepBox` i helpers de confirmació; app tradueix tons/decisions, i executor no coneix colors ni tipus UI.
- [ ] Migrar `executor.RuntimeDeps` de `*ui.Turn` al contracte consumidor, retirar `internal/ui` d’importacions d’executor i adaptar tests amb un presenter fals petit.
- [ ] Executar `go list -f '{{join .Imports "\n"}}' ./internal/executor | rg 'internal/ui'` i confirmar sortida buida; repetir tests d’edició i revalidació de risc.
- [ ] Moure el bucle interactiu a `interactive_loop.go` amb un `interactiveSession` privat que posseeixi configuració activa, mode, historial i `sessionState`.
- [ ] Extraure `routeInteractiveInput`, `executeInteractiveTurn`, `executeManualInput` i una única `applyTurnResult`. Aquesta última és l’únic lloc que actualitza historial, retry, proposta pendent, evidències i memòria després de `runTurn`.
- [ ] Afegir proves de caracterització per completed, blocked, cancelled, error amb execucions parcials i proposta acceptada; cada cas ha de demostrar una sola aplicació d’estat.
- [ ] Moure l’orquestració de torn a `turn.go` i fer que `runTurn` delegui construcció de `planningRoundRequest`, validació/reparació, finalització `complete/blocked`, execució/admissió d’evidències i límit de planificació. `workflowState` continua sent el propietari mutable i no es duplica.
- [ ] Mantenir noms d’esdeveniments, camps de trace, codis de sortida i text renderitzat; qualsevol golden/transcript diferent activa una porta d’aturada.
- [ ] Superar la porta comuna de fase, obligatòriament amb `-race`.

**Done evidence:** executor no importa UI; `applyTurnResult` és l’únic aplicador de sessió; caracterització visual, traces, cancel·lació i reintents és idèntica.

## Fase 8 — Contracte LLM i prompt

**Resultat avançat:** parsing estricte per proveïdors amb `response_format`, compatibilitat local explícita i prompt dividit sense canvi textual.

**Propietari i frontera:** LLM defineix els modes; app selecciona el mode segons `ClientOptions.SupportsResponseFormat`.

**Dependències:** orquestració estabilitzada de la fase 7.

**Fitxers:**

- Create: `internal/llm/response.go`
- Create: `internal/llm/prompt.go`
- Create: `internal/llm/testdata/build_user_prompt.golden`
- Modify: `internal/llm/llm.go`, `export.go`, `llm_test.go`
- Modify: `internal/app/turn.go`, `main_loop_test.go`, `workflow_test.go`

- [ ] Definir `ResponseMode` amb `ResponseModeStrict` i `ResponseModeCompatible`; canviar `ParseResponse` perquè exigeixi el mode explícit.
- [ ] Afegir tests RED: estricte rebutja prefix, sufix, brace extra, dos objectes i document no objecte; compatible conserva el primer objecte, braces dins de strings i el brace final tolerat pels models locals.
- [ ] Implementar mode estricte amb `json.Decoder`: una primera descodificació a `Response`, una segona que ha de retornar EOF i cap text no blanc fora del document. No activar `DisallowUnknownFields`.
- [ ] Mantenir `firstJSONObject` exclusivament per `ResponseModeCompatible` i conservar la validació funcional comuna després de l’extracció.
- [ ] Fer que `runPlanningRound`, inclòs el segon intent de reparació, seleccioni mode estricte quan `SupportsResponseFormat=true` i compatible quan és `false`; afegir test d’integració per tots dos camins.
- [ ] Crear una fixture representativa amb autoritat, context, memòria, evidències, intents, decisió anterior, error de reparació i pressupostos; guardar exactament la sortida actual a `build_user_prompt.golden` abans de moure cap bloc.
- [ ] Moure parsing/validació a `response.go` i construcció de prompts a `prompt.go`. Separar helpers de secció per autoritat/objectiu, historial/context, memòria, evidències/intents, reparació i pressupostos; `buildUserPrompt` només ordena seccions ja construïdes.
- [ ] Executar el test golden després de cada extracció i no regenerar la fixture si canvia el text; una diferència és regressió i activa una porta d’aturada.
- [ ] Executar els tests de contracte existents per acció, objectiu, completion basis i normalització, confirmant que la classificació de risc continua local.
- [ ] Superar la porta comuna de fase.

**Done evidence:** matriu strict/compatible verda, selecció integrada amb capacitat del model i golden del prompt sense diferències.

## Checkpoints agrupats

### Checkpoint A — Estabilització, després de la fase 3

- [ ] Reexecutar els PoC de substitució sense crear efectes laterals reals.
- [ ] Confirmar que un error interactiu retorna codi i tanca trace.
- [ ] Confirmar suite, vet, lint, race i CI localment reproduïbles.
- [ ] No començar identitat/refactorització amb cap incidència de qualitat oberta.

### Checkpoint B — Fronteres, després de la fase 6

- [ ] Confirmar ruta de mòdul canònica i arbre de dependències esperat.
- [ ] Confirmar absència de funcions-àlies i de secrets a executor/UI.
- [ ] Confirmar configuracions antigues, flags, model i tema sense canvis observables.
- [ ] No començar la descomposició si alguna frontera encara rep `config.Config` completa.

### Checkpoint C — Orquestració, després de la fase 8

- [ ] Confirmar executor sense UI, estat de torn amb un únic aplicador i traces estables.
- [ ] Confirmar contractes JSON estricte/compatible i golden de prompt.
- [ ] Executar la verificació final completa abans de declarar el programa acabat.

## Verificació final

- [ ] `env GOCACHE=/tmp/go-build go test -count=1 ./...`
- [ ] `env GOCACHE=/tmp/go-build go vet ./...`
- [ ] `env GOCACHE=/tmp/go-build GOLANGCI_LINT_CACHE=/tmp/shellia-golangci-lint golangci-lint run ./...`
- [ ] `env GOCACHE=/tmp/go-build go test -race -count=1 ./internal/app ./internal/executor ./internal/trace ./internal/ui`
- [ ] `go list ./...`
- [ ] `env GOCACHE=/tmp/go-build go build -o /tmp/shellia-plan-00 ./cmd/shellia`
- [ ] `rg -n "os\\.Exit" cmd internal` mostra només `cmd/shellia/main.go`.
- [ ] `rg -n '\b(APIKey|APIKeyEnv)\b' internal/executor internal/ui` no mostra resultats.
- [ ] `go list -f '{{join .Imports "\n"}}' ./internal/executor | rg 'internal/ui'` no mostra resultats.
- [ ] `rg -n '"shellia/internal/' --glob '*.go'` no mostra resultats.
- [ ] `git diff --check` no mostra errors i, després del commit final de fase, `git status --short` no mostra canvis pendents.

## Portes d’aturada materials

Aturar la fase actual i demanar decisió si:

- cal canviar una API, configuració persistent o sortida no prevista per l’especificació;
- una correcció necessita parser de shell extern, dependència o paquet arquitectònic nou;
- un transcript, codi de retorn, nom/camp de trace o golden canvia sense ser criteri d’acceptació;
- una prova general o de race falla i la causa no és estrictament la fase;
- apareixen canvis locals de l’usuari en un fitxer que la fase ha de modificar;
- la fase necessita avançar treball assignat a una fase posterior;
- la primera execució remota de CI falla per una diferència no reproduïble localment.

## Handoff d’execució

Executar amb `implement-project-phase` i `superpowers:executing-plans`, una fase cada vegada. Al final de cada fase, adjuntar les proves focalitzades, la porta comuna, el commit i qualsevol risc residual; no començar la següent fins que el checkpoint aplicable sigui verd.
