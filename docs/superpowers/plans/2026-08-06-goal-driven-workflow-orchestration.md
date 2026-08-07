# Pla d’implementació de l’orquestració orientada a objectius

**Data:** 2026-08-06

**Estat:** implementat i verificat

**Nivell:** CRITICAL

**Especificació canònica:** [Disseny d’orquestració de workflow orientada a objectius](../specs/2026-08-06-goal-driven-workflow-orchestration-design.md)

## 1. Resultat i acceptació vinculada

El resultat és un únic lifecycle intern que decideix `execute`, `complete` o `blocked`, conserva l’objectiu entre rondes i no delega l’autoritat d’execució ni la finalització a booleans predictius.

Aquest pla agrupa l’acceptació de l’especificació en sis proves de producte:

- **AC1 — Camí curt i plan-only:** resposta directa sense executor, consulta local mínima i `/plan` amb zero invocacions del runner. Cobreix §15.1, punts 1, 2 i 11.
- **AC2 — Continuïtat de l’objectiu:** descoberta → acció → verificació continua després d’èxits i errors ordinaris fins a `complete` o `blocked`. Cobreix §15.1, punts 3 i 4.
- **AC3 — Terminació honesta:** falta d’entrada, timeout, cancel·lació, rebuig, límit i error estructural tenen causes terminals explícites i no es presenten com a èxit. Cobreix §15.1, punts 9 i 10.
- **AC4 — Repetició contextual:** reintents, verificacions i repeticions demanades són possibles; la reiteració sense progrés acaba en reparació o `no_progress`. Cobreix §15.1, punts 5, 6 i 7.
- **AC5 — Context acotat i diagnosticable:** evidència truncada, follow-up després de `missing_input` i traces de decisió/intents/terminació. Cobreix §15.1, punt 8, i §16.
- **AC6 — Autoritat i presentació preservades:** risc local, confirmacions, reclassificació després d’edició, visibilitat de comando/propòsit i renderers actuals. Cobreix §15.2 i §15.3.

La implementació s’atura quan AC1–AC6 estan demostrats i tots els Stop Gates finals estan tancats.

## 2. Nivell i proporcionalitat

Es manté el nivell **CRITICAL** de l’especificació perquè es modifica el camí que concedeix autoritat a l’executor, la recuperació després d’errors i la semàntica de terminació. No es canvia la política de seguretat, però cal demostrar que el nou controlador no la pot evitar.

La unitat d’entrega continua sent una sola feature. No cal dividir-la en fases de projecte: hi ha tres slices ordenats, tots dins dels owners actuals i sense serveis, dependències, persistència o frameworks nous.

Baseline verificat abans del pla:

```text
env GOCACHE=/tmp/go-build go test -count=1 ./internal/app ./internal/llm ./internal/executor ./internal/session ./internal/trace
```

Resultat: tots cinc paquets passen.

## 3. Abast

### Dins d’abast

- contracte estructurat de decisió i validació exacta;
- estat de workflow en memòria propietat d’`internal/app`;
- lifecycle post-batch i terminacions explícites;
- separació estructural de `/plan`;
- admissió contextual de repeticions i detecció d’estancament;
- projecció acotada d’intents/evidència al prompt i a la sessió;
- traces necessàries per demostrar les transicions;
- eliminació del contracte, configuració i branques internes substituïdes;
- ajust mínim de documentació activa si descriu el comportament eliminat.

### Fora d’abast

- model, proveïdor o transport LLM;
- política de risc, significat de `yes_safe` o textos de confirmació normals;
- execució paral·lela, workflows persistents o resum de subtasques en graf;
- canvis a `!` i `/shell`;
- redisseny visual o selector simple/complex;
- compatibilitat interna amb l’orquestrador anterior.

### Diferit

- equivalència semàntica entre comandos textualment diferents;
- reprendre workflows després de reiniciar Shellia;
- streaming parcial del camp de resposta final estructurada;
- telemetria o mètriques noves fora de les traces actuals.

## 4. Propietaris i punts de reutilització

- **Lifecycle:** `internal/app.runTurn`, `runPlanningRound` i `runtimeDeps`. Es conserva la injecció de runner, I/O, client HTTP i trace logger.
- **Estat del workflow:** nou codi dins de `internal/app`, en un fitxer acotat al workflow; no es crea un paquet ni un framework d’estats.
- **Contracte del model:** `internal/llm.Response`, `PromptRequest`, `BuildPrompts`, `parseResponse` i normalització. Es conserva el client OpenAI-compatible actual.
- **Contractes compartits:** `internal/core/types.go` només per a resultats que ja travessen app, executor, sessió, UI o traces. `WorkflowState` no surt d’`internal/app`.
- **Execució:** `internal/executor.ExecuteCommands` i el seu `CommandBatchResult`. Es conserven execució seqüencial, skips per dependència, timeout, detecció interactiva i reclassificació del comando efectiu.
- **Seguretat:** `internal/safety` i la normalització local actual, sense canvis de política.
- **Sessió:** `internal/session.UpdateState` i els camps existents `PendingIntent`/`LastObservations`, ampliats només si l’acceptació no es pot expressar amb els tipus actuals.
- **UI:** plan boxes, command boxes, confirmacions, result renderer i helpers plan-only actuals que continuïn tenint un consumidor real.
- **Traces:** `internal/trace.Logger.Record` i el fitxer JSONL actual; no es crea cap sink nou.

El hotspot actual `runTurn` concentra 253 línies i alta complexitat cognitiva. El workflow nou s’extreu dins del mateix paquet per fer explícites les transicions, mentre `runTurn` conserva coordinació d’I/O i owners existents.

## 5. Ordre d’execució

```text
Slice 1: lifecycle canònic i autoritat de /plan
    ↓
Slice 2: repeticions contextuals i no-progress
    ↓
Slice 3: context acotat, sessió, traces i neteja final
```

No hi ha treball paral·lel segur entre els slices: el segon consumeix l’estat i les transicions del primer, i el tercer projecta els intents definitius del segon.

## 6. Slice 1 — Tall al lifecycle canònic

### Resultat avançat

Entrega AC1, AC2, AC3 i la base d’AC6: Shellia pot respondre, executar o bloquejar-se explícitament; sempre reavalua l’objectiu després d’un batch; `/plan` no té autoritat d’execució.

### Owner i límit afectat

- `internal/app`: `runTurn`, `runPlanningRound`, `runtimeDeps` i nou estat/transicions del workflow;
- `internal/llm`: resposta, prompt, parseig i reparació estructural;
- `internal/core`: outcome terminal compartit i `TurnResult` si sessió/traces el consumeixen;
- `internal/config` i `internal/ui`: eliminació del camí de confirmació plan-only i reutilització dels renderers.

### Dependència

Cap canvi funcional previ. Parteix del baseline verd i del `CommandBatchResult` existent.

### Treball executable

1. Escriure primer proves RED del contracte per a les combinacions vàlides i els rebuigs exactes: `execute` sense comandos, `complete` sense resposta, `blocked` sense causa, comandos en decisions terminals i acció desconeguda.
2. Substituir `llm.Response` per una decisió única amb resposta final o blocker segons l’acció. Consolidar les regles estables del system prompt i deixar a la projecció dinàmica només objectiu, autoritat, pressupost i evidència.
3. Crear dins d’`internal/app` l’estat del torn i transicions petites que mantinguin immutable `executionAllowed`, pressupost de rondes, una reparació estructural i outcome terminal.
4. Tallar `runTurn` al nou lifecycle:
   - `complete` renderitza la resposta amb el renderer actual;
   - `blocked` retorna una causa accionable i resultat no complet;
   - `execute` normalitza el risc local, mostra el mateix pla, conserva les confirmacions i usa l’executor actual;
   - cada batch ordinari torna al planner amb l’evidència real, sense dependre d’una predicció anterior;
   - timeout, cancel·lació i rebuig acaben sense reexecució automàtica;
   - el límit conserva la confirmació actual per ampliar pressupost en mode normal.
5. Derivar `executionAllowed` una sola vegada de `PlanOnly`. En plan-only, una decisió `execute` es mostra com a pla i retorna abans de qualsevol confirmació d’execució o crida al runner.
6. Eliminar del camí viu `requires_observation`, `observation_reason`, el resumidor separat d’execucions i la mutació `PlanOnly=false`.
7. Eliminar `AskConfirmPlanOnly`, el flag `--ask-confirm-plan-only`, la clau de la plantilla de configuració i la seva projecció de trace. Configuracions antigues poden contenir la clau, però no hi ha codi que la llegeixi ni emuli el comportament.
8. Adaptar `TurnResult` i consumidors perquè expressin outcome/blocker explícits. Eliminar `Actionable` si, després del cutover, només representaria compatibilitat amb tests o branques antigues.
9. Preservar els helpers de UI només si continuen renderitzant el pla, el comando/propòsit o la resposta final; eliminar helpers dedicats exclusivament a acceptar i executar plan-only.

### Verificació proporcional

- RED/GREEN a `internal/llm` per schema, parseig, prompt únic i una sola reparació estructural.
- RED/GREEN a `internal/app` amb fake LLM i `runtimeDeps` per:
  - resposta directa sense runner;
  - consulta simple `execute → complete`;
  - èxit `execute → execute → complete`;
  - error ordinari `execute → reparació → complete`;
  - `blocked/missing_input`;
  - timeout, cancel·lació, rebuig i límit;
  - plan-only amb decisions `execute`, `complete`, `blocked` i resposta invàlida, sempre amb runner sentinella que falla si és invocat.
- Regressió a `internal/executor` i `internal/safety` per confirmar que runner, risc, confirmacions i edició mantenen el comportament actual.
- Regressió de UI per comando i propòsit visibles abans d’executar i resposta final al renderer existent.

Comanda de tancament del slice:

```text
env GOCACHE=/tmp/go-build go test -count=1 ./internal/llm ./internal/app ./internal/executor ./internal/safety ./internal/config ./internal/ui
```

### Evidència de done

- AC1–AC3 passen amb fake LLMs deterministes.
- No existeix cap crida al runner des del branch plan-only.
- Un èxit de comando ja no tanca l’objectiu per si sol.
- `requires_observation`, el resumidor separat i la confirmació plan-only no tenen cap consumidor de producció.
- Les confirmacions normals i la classificació local passen la regressió sense canvis.

## 7. Checkpoint A — Autoritat i terminació

No començar el Slice 2 si falla algun punt:

- una resposta del model pot modificar `executionAllowed`;
- `/plan` pot arribar al runner per un camí normal, de reparació o d’error;
- una decisió invàlida pot acabar executant comandos;
- timeout, cancel·lació, rebuig o límit apareixen com a `complete`;
- el model pot reduir risc o eliminar una confirmació;
- `runTurn` conserva dos motors de lifecycle funcionals.

## 8. Slice 2 — Repeticions contextuals i estancament

### Resultat avançat

Entrega AC4 i completa la part de repetició d’AC6: es pot repetir feina quan la causa és explícita, però una repetició sense evidència ni progrés no crea un bucle ni una negativa persistent.

### Owner i límit afectat

- `internal/app`: ledger d’intents, revisió d’evidència, admissió i transició `no_progress`;
- `internal/llm`: `repeatReason` tipat i regles de planificació;
- `internal/core`: camp compartit a `CommandPlan` només perquè app i executor el consumeixen;
- `internal/executor`: defensa després d’editar el comando efectiu.

### Dependència

Requereix l’estat, els outcomes i la replanificació post-batch del Slice 1.

### Treball executable

1. Escriure proves RED per reintent d’un error, verificació després d’una mutació, polling d’estat canviable, repetició explícita de l’usuari, duplicat sense causa, duplicat dins del batch i edició cap a un èxit previ.
2. Afegir al contracte final les causes tancades `user_requested`, `retry`, `verify_after_change` i `poll_changed_state`. Rebutjar valors desconeguts; el camp buit continua sent conservador.
3. Completar el ledger en memòria amb identificador/ronda, comando planificat i efectiu, propòsit, outcome d’execució, revisió d’evidència, causa de repetició i relació causal quan existeixi. Reutilitzar `CommandExecution`/`SkippedCommand` i no duplicar el text complet de sortida.
4. Substituir `filterPreviouslySuccessfulPlans` i la blacklist global d’èxits per admissió contextual a `internal/app`:
   - fallits i saltats continuen sent reintentables;
   - un èxit necessita causa tipada si el comando efectiu coincideix;
   - la causa no altera risc, confirmació ni independència després d’error;
   - una proposta no admesa es converteix en evidència de reparació, no en execució.
5. Mantenir a l’executor una comprovació final després de l’edició. Si l’edició crea un duplicat sense causa, es registra com a skip/estancament; editar no autoritza implícitament la repetició.
6. Incrementar `evidenceRevision` només quan hi ha entrada nova o outcome real d’execució. Una justificació del model, per si sola, no és evidència de progrés.
7. Aplicar el pressupost d’estancament: una reparació consecutiva; si la decisió següent repeteix el conflicte sense progrés, acabar amb `no_progress` i conservar els resultats parcials.
8. Eliminar constants, branques i tests que codifiquen “qualsevol èxit previ és irrepetible”, substituint-los per la matriu contextual.

### Verificació proporcional

- Taula RED/GREEN de totes les causes admeses i rebutjades a `internal/llm` i `internal/app`.
- Runner comptador que demostri exactament quantes vegades s’executa cada comando.
- Prova de repetició perillosa que demostri la mateixa classificació i confirmació a cada intent.
- Prova d’edició que demostri reclassificació i admissió sobre el comando efectiu.
- Prova de dues propostes sense progrés que acabi en `no_progress` dins del pressupost.
- Regressió de batch dependent/independent i skips reals a `internal/executor`.

Comanda de tancament del slice:

```text
env GOCACHE=/tmp/go-build go test -count=1 ./internal/llm ./internal/app ./internal/executor ./internal/safety
```

### Evidència de done

- AC4 passa per totes les causes tipades.
- La justificació de repetició no apareix en cap càlcul de risc ni bypass de confirmació.
- No queda cap blacklist global d’èxits ni missatge rígid d’“already completed” com a decisió terminal.
- El workflow sempre surt per progrés, bloqueig o pressupost finit.

## 9. Checkpoint B — Repetició segura

No començar el Slice 3 si falla algun punt:

- un `repeatReason` pot baixar risc o evitar confirmació;
- una cadena de repeticions pot créixer sense `evidenceRevision` ni límit;
- un comando editat evita la comprovació final;
- una repetició demanada explícitament continua bloquejada;
- un duplicat accidental s’executa sense causa o queda silenciosament descartat sense outcome.

## 10. Slice 3 — Context acotat, sessió i diagnòstic

### Resultat avançat

Entrega AC5 i tanca AC6: el planner conserva el fil causal sense prompt il·limitat, els follow-ups reprenen bloquejos útils, i les traces permeten demostrar què va decidir el workflow sense alterar la UI.

### Owner i límit afectat

- `internal/llm`: projecció dinàmica d’intents i evidència;
- `internal/app`: construcció de la projecció i events de lifecycle;
- `internal/session`: memòria compacta de completion/blocker;
- `internal/trace`: dades noves sobre events existents o events de workflow acotats;
- `internal/ui`: compatibilitat final de presentació;
- documentació/configuració activa: neteja del comportament eliminat.

### Dependència

Requereix el ledger i les causes terminals definitives dels Slices 1 i 2.

### Treball executable

1. Escriure proves RED per pressupost global d’evidència, última ronda sempre present, errors recents preservats, marca de truncament i sortida de comando tractada com a evidència no fiable.
2. Construir una projecció, no una transcripció: objectiu i autoritat sempre; decisió i batch més recents sempre; errors necessaris; intents anteriors resumits fins al límit; marca explícita quan s’omet contingut.
3. Reutilitzar `ObservationOutputChars`, `MaxObservationEntries` i les metadades de truncament actuals. No afegir una configuració nova si aquests límits poden expressar l’acceptació.
4. Actualitzar `internal/session.UpdateState` perquè:
   - `blocked/missing_input` conservi `PendingIntent`, blocker i observacions útils per al follow-up;
   - `complete` projecti el resultat final i netegi el blocker resolt;
   - cancel·lació, rebuig, timeout, límit i error estructural no es guardin com a èxit;
   - la memòria continuï sent curta i no copiï el ledger complet.
5. Registrar a les traces inici/autoritat, decisió per ronda, intent, admissió de repetició, revisió d’evidència, truncament i outcome terminal. Mantenir l’opt-in i la política actuals de contingut sensible.
6. Adaptar els tests de UI i traces a la resposta final no streaming sense canviar boxes, propòsits, confirmacions o estil de resposta.
7. Eliminar aliases, exports, helpers, proves i configuració que hagin quedat sense consumidor després del cutover. No conservar wrappers per compatibilitat interna.
8. Actualitzar només la documentació activa que encara descrigui confirmació/executabilitat de plan-only o el lifecycle antic. Els documents històrics de disseny poden conservar referències contextuals.

### Verificació proporcional

- RED/GREEN a `internal/llm` per projecció, límits i instruccions no fiables.
- RED/GREEN a `internal/session` per `missing_input → follow-up → complete` i terminacions no exitoses.
- RED/GREEN a `internal/trace`/`internal/app` per seqüència d’events, causes terminals i truncament.
- Regressió de UI per pla, comando/propòsit, confirmacions i resposta final.
- Cerca d’absència en codi viu i documentació activa dels contractes eliminats.

Comanda de tancament del slice:

```text
env GOCACHE=/tmp/go-build go test -count=1 ./internal/llm ./internal/app ./internal/session ./internal/trace ./internal/ui ./internal/config
```

### Evidència de done

- AC5 passa amb prompts acotats i truncament visible.
- Un follow-up completa l’objectiu pendent sense perdre el blocker que l’ha originat.
- Cada torn té autoritat, decisions, intents i causa terminal diagnosticables.
- No queda codi viu de `requires_observation`, resumidor separat, confirmació plan-only o blacklist global.
- AC6 passa sense canvi de política de seguretat ni de presentació prèvia a l’execució.

## 11. Checkpoint C — Integració completa

Abans de la verificació final:

- executar una matriu determinista de petició directa, consulta local, workflow complex, error/reparació, missing input/follow-up, repetició admesa, estancament, timeout, cancel·lació, rebuig, límit i `/plan`;
- inspeccionar que cada cas acaba amb un outcome i causa coherents;
- confirmar que el nombre de crides al model i al runner coincideix amb les transicions esperades;
- confirmar que l’evidència truncada és explícita i que el prompt no creix amb el ledger complet;
- revisar que només hi ha un lifecycle de producció.

## 12. Verificació final

Executar una sola seqüència completa després de tancar els tres slices:

```text
gofmt -w ./cmd ./internal
env GOCACHE=/tmp/go-build go test -count=1 ./...
go build -o /tmp/shellia-goal-workflow ./cmd/shellia
git diff --check
rg -n "requires_observation|AskConfirmPlanOnly|ask_confirm_plan_only|streamSummarizeExecutions|filterPreviouslySuccessfulPlans" internal cmd README.md site
```

La cerca final ha de retornar zero coincidències en codi viu i documentació activa. No s’aplica als documents històrics de `docs/superpowers`.

Fer una única revisió independent del diff final, centrada en:

- immutabilitat de `executionAllowed`;
- impossibilitat que `/plan` arribi al runner;
- classificació local i confirmacions després de replanificar, repetir o editar;
- terminacions falsament exitoses;
- pressupostos de ronda, reparació i estancament;
- exposició accidental de més contingut a traces o prompts.

Qualsevol troballa d’autoritat o seguretat reobre el checkpoint corresponent; una troballa de presentació només reobre AC6.

## 13. Stop Gates materials

No lliurar ni marcar la feature com a completa si:

1. `/plan` pot invocar l’executor en qualsevol variant.
2. El model pot modificar autoritat, reduir risc o ometre una confirmació local.
3. Coexisteixen dos lifecycles funcionals o un flag per seleccionar-los.
4. `requires_observation` continua governant la continuació.
5. Un èxit de comando es tracta com a completion de l’objectiu.
6. Timeout, cancel·lació, rebuig, límit o error estructural apareixen com a èxit.
7. Una repetició justificada evita la seguretat o una repetició sense progrés pot iterar indefinidament.
8. L’evidència del prompt no té pressupost global o no indica truncament.
9. Comando i propòsit deixen de ser visibles abans de l’execució.
10. Queden camps, helpers, flags, configuració o tests antics només per compatibilitat interna.
11. La suite completa, el build o la revisió independent de risc no passen.

## 14. Traspàs

Aquest pla s’ha d’entregar a `implement-project-feature`, no a `implement-project-phase`: és una sola feature CRITICAL amb tres slices dependents dins de l’arquitectura actual.

La implementació ha de seguir l’ordre dels slices i aturar-se a cada checkpoint si falla una prova d’autoritat, recuperació o seguretat.
