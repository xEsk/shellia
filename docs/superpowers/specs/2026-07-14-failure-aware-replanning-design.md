# Disseny de replanificació conscient d'errors

Data: 2026-07-14
Estat: disseny aprovat; document pendent de revisió final

## 1. Objectiu i resultat observable

Quan una ordre d'un pla generat per la IA falla, Shellia ha de poder usar el resultat real de l'error per preparar un pla de recuperació segur. La recuperació ha de mantenir el control de l'usuari, respectar els límits de planificació existents i evitar executar passos que depenen d'un prerequisit fallit.

El resultat observable és:

- els errors ordinaris mostren l'ordre, el codi de sortida i la sortida capturada;
- els passos bloquejats es mostren com a `skipped` i no s'executen;
- els passos explícitament independents poden continuar quan `continue_on_error=true`;
- després d'un lot amb almenys un error ordinari, Shellia replanifica una vegada amb tota l'evidència del lot;
- el pla de recuperació passa per les mateixes regles de classificació, presentació i confirmació que qualsevol altre pla;
- el resum final diferencia execucions reeixides, fallides i passos omesos.

La millora és de complexitat `MEDIUM`: afecta el contracte del model, els tipus compartits, l'executor, el bucle de planificació, la UI, les traces i les proves, però reutilitza l'arquitectura actual i no incorpora persistència, infraestructura ni dependències noves.

## 2. Comportament actual i reutilització

El flux existent `planificar → confirmar → executar → observar → replanificar → resumir` viu a `internal/app/main.go`. Ja proporciona:

- acumulació d'execucions del torn;
- rondes de planificació limitades per `planning_max_rounds`;
- diàleg per ampliar el límit;
- classificació local i confirmació dels plans;
- replanificació quan el model havia declarat `requires_observation`;
- resum final basat en execucions reals.

`internal/executor/executor.go` ja distingeix errors ordinaris, timeouts, cancel·lacions i prompts interactius. També implementa `continue_on_error`, però actualment, quan és `true`, executa indiscriminadament tots els passos restants.

`internal/llm/llm.go` defineix el contracte JSON, normalitza els plans i construeix les observacions i el resum. Les observacions de planificació actuals inclouen stdout i stderr, però no el codi de sortida.

Es reutilitzaran sense canvis conceptuals:

- `PlanningMaxRounds` i el seu diàleg de continuació;
- la classificació local de seguretat;
- la confirmació de pla i de cada ordre;
- `yes_safe`;
- els límits de captura i truncament de stdout/stderr;
- `runtimeDeps` i els clients LLM falsos de les proves;
- el logger genèric de traces.

## 3. Contracte d'acceptació

### Inici

La recuperació comença quan una ordre d'un pla d'IA acaba amb un error ordinari d'execució. No s'aplica a ordres manuals executades amb `!` o `/shell`.

### Execució del lot

- Amb `continue_on_error=false`, el primer error o timeout atura el lot.
- Amb `continue_on_error=true`, després d'un error o timeout:
  - els passos posteriors són dependents per defecte;
  - els passos dependents es registren com a `skipped`;
  - només s'executen els passos marcats explícitament com a independents dels errors anteriors del mateix lot.
- Una cancel·lació atura immediatament el torn i no executa cap altre pas.
- Els passos `skipped` no passen per confirmació i no produeixen efectes, codi de sortida ni sortides fictícies.

### Recuperació

- Cada lot amb almenys un error ordinari provoca exactament una nova ronda de planificació.
- Un lot pot contenir més d'un error ordinari si continuen passos independents; encara provoca una sola ronda.
- Un error ordinari força la ronda encara que el pla anterior no declarés `requires_observation`.
- Un timeout no provoca replanificació automàtica.
- Una cancel·lació no provoca replanificació automàtica.
- Un error nou en un pla de recuperació pot provocar una altra ronda.
- Cada ronda consumeix `planning_max_rounds` i reutilitza el diàleg existent quan arriba al límit.

### Seguretat i confirmació

- Els plans de recuperació es normalitzen i classifiquen localment igual que els plans inicials.
- Les confirmacions de pla i d'ordre es mantenen.
- `yes_safe` conserva el comportament actual i és l'única via d'autoexecució de passos localment segurs.
- La declaració d'independència del model no modifica el risc ni pot reduir una confirmació requerida localment.

### Repeticions

- Una ordre que ja ha acabat correctament en el mateix torn no es torna a executar.
- Una ordre fallida es pot tornar a proposar i executar en una ronda posterior.
- Un pas `skipped` no compta com a executat i pot aparèixer en un pla posterior.
- La identitat continua sent la cadena efectiva de l'ordre després de normalitzar espais. Si l'usuari edita una ordre, es registra i compara l'ordre editada, no l'original.

### Resultat i memòria

- L'observació de la ronda següent inclou ordre, propòsit, codi de sortida, stdout i stderr de les execucions, més ordre, propòsit i motiu dels passos `skipped`.
- El resum final rep execucions i omissions separadament i no pot presentar una fallida o omissió com a completada.
- Només les execucions reals entren a les observacions reutilitzables de sessió.
- Només les execucions reeixides poden alimentar detecció de fitxers creats o altres efectes de sessió.

## 4. Abast

### Inclòs

- camp de dependència per ordre al contracte del model;
- resultat explícit de lot amb execucions i omissions;
- continuació selectiva després d'errors i timeouts;
- replanificació automàtica després d'errors ordinaris;
- observacions amb codis de sortida i passos omesos;
- filtratge individual de repeticions reeixides;
- UI i traces per a passos `skipped` i decisions de recuperació;
- actualització de documentació i comentaris de configuració.

### Exclòs

- recuperació automàtica de timeouts o cancel·lacions;
- replanificació d'ordres manuals;
- inferència local de dependències entre ordres;
- canvis en la classificació de seguretat;
- configuracions o flags nous;
- persistència de recuperacions entre sessions;
- una màquina d'estats o servei de recuperació nou;
- lògica especial per Git o per missatges d'error concrets.

## 5. Model de dades i flux

### Contracte del model

`llm.Command` i `core.CommandPlan` incorporaran un booleà amb semàntica explícita:

```text
independent_on_failure
```

El camp només es refereix a errors de passos anteriors del mateix lot. En començar una ronda nova, no hi ha cap dependència implícita respecte del lot anterior. L'absència del camp equival a `false`, cosa que manté compatibles les respostes antigues amb un valor conservador.

Els prompts de sistema i usuari han d'indicar que el model només pot posar `true` quan l'ordre és segura i útil encara que qualsevol pas anterior del mateix lot no s'hagi completat.

### Resultat d'execució

El límit executor/aplicació retornarà un resultat de lot en lloc de sobrecarregar `[]CommandExecution` i `error`:

```text
CommandBatchResult
├── Executions         []CommandExecution
├── Skipped            []SkippedCommand
├── HadOrdinaryFailure bool
└── HadTimeout         bool
```

`SkippedCommand` contindrà com a mínim:

```text
Command
Purpose
Reason
```

Els errors ordinaris i timeouts de les ordres s'expressaran al resultat del lot. El valor `error` de Go queda reservat per a cancel·lacions, avortaments de l'usuari i errors estructurals que impedeixin continuar el flux amb seguretat.

### Flux complet

1. El model emet un pla amb `independent_on_failure`.
2. `parseResponse` valida els camps obligatoris existents.
3. `normalizePlan` conserva la independència i aplica la classificació local.
4. `runTurn` presenta el pla i obté les confirmacions existents.
5. L'executor recorre el lot i manté si ja hi ha hagut un error o timeout.
6. Després d'aquest punt, omet passos dependents i només presenta/executa els independents quan `continue_on_error=true`.
7. L'executor retorna totes les execucions reals, omissions i indicadors agregats.
8. `runTurn` acumula els resultats del torn.
9. Si `HadOrdinaryFailure=true`, força una nova ronda dins del límit.
10. La nova petició inclou execucions i omissions com a observacions de la tasca actual.
11. Abans d'executar el pla nou, Shellia elimina individualment les ordres que ja van acabar amb èxit; no elimina fallides ni omissions.
12. Quan el torn acaba, el resum rep totes les execucions i omissions.
13. La memòria de sessió conserva només observacions d'execucions reals; els efectes derivats només consideren exit code zero.

## 6. Walking skeleton i slices ordenades

### Checkpoint inicial de punta a punta

Una prova de `runTurn` amb client LLM fals retorna primer un pla `[fallida, dependent, independent]`. Amb `continue_on_error=true`, Shellia executa la fallida i la independent, registra la dependent com a `skipped`, envia una segona petició amb tota l'evidència i executa un pla de recuperació sota les confirmacions normals.

Aquest checkpoint demostra el contracte complet abans d'afegir variants.

### Slice 1: contracte i normalització

- Estat: camp absent o present al JSON del model.
- Límit: `llm.Command → core.CommandPlan`.
- RED: el camp no es conserva o l'absència permet continuar.
- GREEN: el camp es conserva i l'absència equival a dependent.
- Dependència: cap.

### Slice 2: lot conscient de dependències

- Estat: èxit, error ordinari, timeout i pas dependent/independent.
- Límit: executor.
- RED: s'executen passos dependents o no es poden representar omissions.
- GREEN: el resultat separa execucions i omissions i conserva els indicadors d'error.
- Dependència: slice 1.

### Slice 3: replanificació per error

- Estat: lot amb error ordinari.
- Límit: `runTurn → prompt d'observació`.
- RED: el torn passa directament al resum.
- GREEN: es produeix una ronda nova encara que `requires_observation=false`.
- Dependència: slice 2.

### Slice 4: límits i exclusions

- Estat: timeout, cancel·lació, límit acceptat i límit rebutjat.
- Límit: executor i bucle de rondes.
- RED: timeout/cancel·lació replanifiquen o el límit no s'aplica.
- GREEN: exclusions i diàleg coincideixen amb el contracte.
- Dependència: slice 3.

### Slice 5: repeticions

- Estat: pla mixt amb ordre reeixida repetida, ordre fallida repetida i ordre nova.
- Límit: filtratge previ a execució.
- RED: es repeteix l'èxit o s'elimina el reintent fallit.
- GREEN: només s'elimina individualment l'èxit redundant.
- Dependència: slice 3.

### Slice 6: projeccions de resultat

- Estat: torn amb èxits, fallides i omissions.
- Límit: UI, traces, resum i sessió.
- RED: una omissió sembla una execució o contamina la memòria.
- GREEN: cada consumidor rep només la representació que li correspon.
- Dependència: slices 2 i 3.

## 7. Riscos i recuperació

### Declaració incorrecta d'independència

El model pot marcar erròniament un pas com a independent. El risc queda limitat perquè:

- `continue_on_error` és `false` per defecte;
- la classificació local no canvia;
- les confirmacions normals continuen aplicant-se;
- `yes_safe` només s'aplica a ordres classificades localment com a segures.

### Bucles de recuperació

Una fallida es pot reintentar, però cada replanificació consumeix una ronda. `planning_max_rounds` i el diàleg d'ampliació són el control únic; no s'afegeix un segon comptador.

### Compatibilitat de `continue_on_error`

El canvi és intencionadament més conservador. Les respostes de models antics no tindran `independent_on_failure`, de manera que, després d'un error i amb `continue_on_error=true`, els passos posteriors es consideraran dependents i s'ometran. La documentació ha d'explicar aquest canvi de semàntica.

### Resultats ficticis

Els passos `skipped` no s'han de representar amb un exit code artificial ni inserir dins de `CommandExecution`. Una estructura separada evita contaminar resums, memòria i detecció d'efectes.

### Recuperació davant fallades del resum

La resposta estàtica de fallback ha de conservar la mateixa regla: mai afirmar que un pas fallit o omès s'ha completat. No cal cap mecanisme nou de retry del resum.

## 8. Estratègia de verificació

### Focal durant el desenvolupament

- `internal/llm`: parseig compatible, normalització i regles del prompt.
- `internal/executor`: combinacions de `continue_on_error`, dependència, error, timeout i cancel·lació.
- `internal/app`: walking skeleton, ronda forçada, límits, confirmacions, `yes_safe` i repeticions.
- `internal/ui`: representació curta de `skipped`.
- `internal/trace`: esdeveniments i camps nous.
- `internal/session`: exclusions de passos omesos i d'efectes d'execucions fallides.

### Proves de frontera

- un lot amb múltiples errors ordinaris només provoca una ronda;
- un timeout amb `continue_on_error=true` només deixa continuar independents i no replanifica;
- una cancel·lació no deixa continuar cap ordre;
- un pla de recuperació arriscat conserva confirmació;
- una ordre segura del pla de recuperació només s'autoexecuta amb `yes_safe`;
- acceptar o rebutjar l'ampliació del límit conserva els resultats acumulats;
- un pla mixt filtra èxits repetits sense perdre ordres noves o fallides reintentables;
- el resum rep omissions separades i la sessió no les persisteix.

### Prova visible integrada

La prova principal amb `runtimeDeps` i client LLM fals ha d'assertar:

- ordre real d'execució;
- estat visible de fallida i `skipped`;
- contingut acotat de la segona petició;
- exactament una ronda de recuperació per lot fallit;
- aplicació de confirmacions al pla recuperat;
- resultat final sense èxits inventats.

### Gates finals

```bash
env GOCACHE=/tmp/go-build go test -count=1 ./...
go build -o shellia ./cmd/shellia
```

No cal una prova amb un proveïdor LLM real: els clients falsos existents permeten verificar el contracte de manera determinista.

## 9. Documentació, compatibilitat i decisions

S'han d'actualitzar:

- `README.md`: funcionalitat, flux, `continue_on_error`, controls de planificació i exclusions;
- comentaris de la configuració generada a `internal/config/config.go`;
- `site/index.html` només si replica o explica `continue_on_error`;
- descripció de traces si el projecte n'afegeix una durant la implementació.

Es mantenen:

- claus i defaults de configuració;
- flags CLI;
- `planning_max_rounds` i la seva variable d'entorn;
- política de confirmació;
- format de l'envolupant OpenAI-compatible;
- comportament d'ordres manuals;
- absència d'estat Git ambiental.

El camp JSON nou és additiu. La seva absència és compatible i conservadora. No hi ha migracions ni canvis de dades persistides.

Les traces incorporaran:

- `command_skipped`, amb pas, ordre, propòsit i motiu;
- una decisió explícita de replanificació provocada per error;
- motius d'exclusió per timeout o cancel·lació;
- metadades d'independència al resultat del planificador.

## 10. Stop Gates i handoff d'implementació

Cal aturar la implementació i tornar al disseny si apareix alguna d'aquestes condicions:

- representar el contracte requereix una màquina d'estats o servei nou;
- la independència pot reduir la classificació o confirmació local;
- no es poden separar omissions i execucions sense alterar APIs externes del projecte;
- cal una configuració o migració nova;
- la recuperació de timeouts o ordres manuals esdevé necessària per completar el contracte;
- el filtratge de repeticions no pot mantenir reintents fallits sense permetre repetir èxits.

Si no apareix cap Stop Gate, aquesta especificació es lliura a `implement-project-feature`. La implementació ha de seguir les slices en ordre, començar pel walking skeleton amb proves i evitar refactors no necessaris.
