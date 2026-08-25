# Disseny del cicle conversacional proposta → pla → execució

**Data:** 2026-08-25

**Estat:** aprovat per implementar

**Nivell:** CRITICAL

**Àmbit:** ofertes estructurades, plans demanats en llenguatge natural, continuacions de sessió i autoritat d’execució

## 1. Resultat

Shellia podrà passar d’una resposta informativa a un pla executable i, en un torn
posterior, a l’execució segura de l’objectiu sense perdre el compromís adquirit ni
confondre una petició de planificació amb autoritat per executar.

El flux observable serà:

```text
resposta + oferta de pla
    → pla visible amb ordres concretes i zero execucions
    → acceptació explícita d’execució
    → nou workflow executable amb pla visible, classificació local i confirmacions
```

La feature resol específicament converses com aquesta:

1. Shellia diagnostica pressió de memòria.
2. L’usuari pregunta si es pot alliberar RAM sense reiniciar.
3. Shellia respon i ofereix preparar un pla executable.
4. L’usuari diu «ok, quins passos són?» o «fes un pla per fer-ho».
5. Shellia mostra ordres concretes; no les executa.
6. L’usuari diu «executa’l».
7. Shellia replanteja l’objectiu amb l’estat actual i usa el workflow executable
   normal, incloses totes les confirmacions locals.

## 2. Usuari, problema i valor

L’usuari és l’operador que treballa en una sessió interactiva i espera que els
seguiments curts conservin el significat del torn anterior.

Avui el contracte té quatre operacions (`answer`, `observe`, `act`, `capability`) i
quatre accions (`execute`, `retrieve_context`, `complete`, `blocked`). Les ofertes
estructurades només són vàlides per a `capability`. Una resposta `answer` pot acabar
amb «si vols, et dic els passos», però aquesta promesa només existeix al Markdown.
Una petició posterior de pla tampoc té una decisió pròpia: el model pot retornar un
`answer/complete` genèric o un `execute` que entra al workflow executable.

La conseqüència és doble:

- Shellia pot repetir explicacions sense lliurar les ordres promeses;
- l’única representació actual d’un pla amb ordres és també la representació d’un
  intent d’execució, llevat que el torn ja hagués començat explícitament amb `/plan`.

El valor immediat és obtenir un lliurable operatiu i verificable, mantenint una
separació estricta entre «prepara’m el pla» i «executa l’objectiu».

## 3. Comportament actual i reutilització

La feature amplia els propietaris existents, sense crear un segon router ni un nou
motor de workflow:

- `internal/llm` ja defineix l’esquema, el prompt, la validació de respostes i la
  normalització local dels plans;
- `internal/app/turn.go` ja renderitza un pla i talla abans de l’executor quan
  `executionAllowed=false`;
- `internal/app/workflow.go` ja és l’autoritat de la matriu acció/operació i dels
  resultats terminals;
- `internal/session` ja desa `PendingProposal`, resol acceptacions inequívoces i
  conserva objectius per a `/retry`;
- `internal/app/interactive_loop.go` ja converteix una proposta acceptada en
  `ResolvedInstruction` i inicia un workflow nou;
- `internal/ui` ja pot mostrar plans i ordres amb els quatre estils visuals;
- `internal/safety` i `internal/executor` continuen sent les úniques autoritats per
  classificar, confirmar i executar comandos.

No es desa ni es reexecuta el text d’un comando extret d’una resposta anterior. Es
reutilitza el patró actual de les ofertes de capacitat: la sessió conserva un
objectiu compacte i el torn acceptat torna a planificar.

## 4. Decisions de producte

### 4.1 Un pla és un resultat no executable

S’afegeix `action=plan`. Aquesta acció inclou ordres i propòsits concrets, però el
runtime mai no la passa a l’executor, encara que el torn normal tingui autoritat
d’execució o `--yes-safe` estigui activat.

`action=plan` és vàlida només amb `operation=act|observe`. L’operació descriu
l’objectiu subjacent; l’acció descriu que el lliurable actual és el pla, no el canvi
ni l’observació.

El resultat del torn és `planned`, reutilitza el renderer actual i conté almenys una
ordre concreta. Una llista conceptual sense ordres no és un pla vàlid.

El mode explícit `/plan` conserva zero autoritat dins del torn, però passa a
participar en el mateix cicle conversacional: després de mostrar un pla correcte pot
publicar una proposta `execute` per a un torn posterior. Això substitueix de manera
intencionada l’absència actual de continuació; no reintrodueix l’antiga possibilitat
d’executar dins del mateix torn plan-only.

### 4.2 Les ofertes tenen un mode explícit

`offer` afegeix `mode`:

```text
offer.mode: "" | plan | execute
offer.objective: objectiu complet del següent workflow
offer.summary: descripció curta del lliurable ofert
```

Combinacions vàlides:

| Decisió actual | Oferta permesa | Significat |
| --- | --- | --- |
| `answer + complete` | `plan` | La resposta està resolta i Shellia pot preparar un pla concret com a pas següent. |
| `capability + complete` | `execute` | La capacitat està explicada i Shellia pot intentar l’objectiu en un torn nou. |
| `act/observe + plan` | `execute` | El pla està lliurat i es pot oferir executar l’objectiu subjacent. |

La resta de combinacions amb una oferta no buida són estructuralment invàlides. Una
oferta no pot aparèixer en `blocked`, `retrieve_context` o `execute`.

Una resposta no ha d’oferir allò que l’usuari ja ha demanat. Si l’usuari demana
passos exactes, ordres o un pla executable, la decisió ha de ser `action=plan`, no
una explicació que torni a oferir el mateix lliurable.

### 4.3 Acceptar un pla no preautoritza les ordres mostrades

La sessió conserva `PendingProposal{Mode, Objective, Summary}`, no `CommandPlan`.
Quan s’accepta una proposta `execute`, Shellia crea un workflow executable nou amb
l’objectiu ofert. El model torna a planificar amb el context actual, les ordres es
tornen a normalitzar i la política local torna a decidir risc i confirmacions.

Aquesta decisió evita:

- executar ordres obsoletes després que canviïn el directori o l’estat del sistema;
- convertir text del model en una autorització durable;
- haver de persistir plans, fingerprints de context o snapshots de sortida;
- introduir un segon camí d’execució paral·lel a l’executor actual.

El pla pot tornar-se a mostrar abans de l’execució. Aquesta repetició és intencionada:
la primera presentació és el lliurable demanat; la segona és el pla actual que entra
al flux de confirmació.

## 5. Contracte estructurat

La resposta del proveïdor passa a ser conceptualment:

```json
{
  "action": "execute|plan|retrieve_context|complete|blocked",
  "operation": "answer|observe|act|capability",
  "success_criteria": "objectiu autoritatiu exacte",
  "summary": "resum del pla o resposta final",
  "context_refs": [],
  "offer": {
    "mode": "|plan|execute",
    "objective": "",
    "summary": ""
  },
  "blocker_kind": "",
  "blocker_reason": "",
  "commands": []
}
```

### 5.1 Validació d’`action=plan`

Una decisió de pla és vàlida només si:

- `operation` és `act` o `observe`;
- `summary` no és buit;
- conté almenys una ordre amb `command` i `purpose`;
- `context_refs`, `blocker_kind` i `blocker_reason` són buits;
- `offer.mode=execute`;
- `offer.objective` descriu l’objectiu executable subjacent i no és buit;
- `offer.summary` no és buit.

Les ordres passen per `NormalizePlan` i per la classificació local igual que un
`execute`, però aquesta classificació només serveix per presentar un pla fidel i
preparar el torn posterior. No concedeix autoritat.

### 5.2 Validació d’ofertes

- `mode=""` exigeix `objective=""` i `summary=""`;
- `mode=plan|execute` exigeix objectiu i resum no buits;
- `answer + complete` només pot usar `mode=plan`;
- `capability + complete` només pot usar `mode=execute`;
- `action=plan` només pot usar `mode=execute`;
- una oferta mai no permet comandos en una decisió que avui els prohibeix.

El JSON Schema estricte i el parser compatible s’actualitzen junts. No es manté un
contracte antic alternatiu.

## 6. Fluxos canònics

### 6.1 Resposta que ofereix preparar el pla

1. L’usuari pregunta si existeix una manera d’aconseguir un resultat.
2. El model retorna `answer + complete` i resol la pregunta.
3. Si hi ha un pas adjacent concret, retorna `offer.mode=plan` amb l’objectiu complet.
4. El runtime desa la proposta només si la decisió completa és admesa.
5. La UI mostra el resum de la proposta i afegeix «Would you like me to prepare an
   executable plan?».

No s’accepten frases lliures com «si vols...» sense una oferta estructurada quan el
model pretén crear una continuació. El text pot existir com a part de l’explicació,
però no tindrà efecte de sessió si `offer` és buit.

### 6.2 Petició explícita de pla

1. L’usuari diu «fes un pla», «dona’m les ordres exactes» o un equivalent semàntic.
2. El model retorna `action=plan`, `operation=act|observe` i ordres concretes.
3. El runtime normalitza i renderitza el pla.
4. L’executor rep zero invocacions.
5. La proposta `execute` de la decisió queda pendent.
6. La UI mostra el resum de la proposta i afegeix «Would you like me to execute it?».

La interpretació continua dins de la decisió de planificació existent. No s’afegeix
un classificador local de frases ni una segona crida de routing.

### 6.3 Acceptació exacta d’una oferta de pla

1. Hi ha una proposta pendent amb `mode=plan`.
2. Una acceptació local inequívoca (`sí`, `ok`, `fes-ho`, equivalents ja admesos)
   resol l’objectiu ofert.
3. El nou torn neix amb autoritat immutable `plan_only`.
4. Si el model retorna `execute`, Shellia mostra el pla i talla abans de confirmacions
   i executor, com `/plan`.
5. En obtenir `planned`, la proposta consumida es promociona a `mode=execute` amb el
   mateix objectiu.
6. La UI ofereix executar-lo en un torn posterior.

Un seguiment més ric com «ok, quins passos són?» no és una acceptació d’execució.
Arriba al model amb la proposta visible i només pot produir el pla o una resposta no
executable. No s’afegeixen heurístiques locals multilingües que puguin executar per
error.

### 6.4 Acceptació d’execució

1. Hi ha una proposta pendent amb `mode=execute`.
2. Una acceptació local inequívoca —inclòs `executa’l` després de normalitzar-la— es
   resol contra `offer.objective`.
3. El nou torn usa l’autoritat normal de la sessió, no la del torn de pla anterior.
4. El provider torna a proposar les ordres actuals.
5. `internal/safety` les classifica localment i l’executor aplica el flux habitual de
   visibilitat, edició, confirmació, timeout i cancel·lació.

Sense proposta `execute` pendent, un «sí» o «executa’l» no inventa cap objectiu.

### 6.5 Rebuig, substitució i nova sessió

- Rebutjar una proposta `plan` mostra «Okay. I won't prepare the plan.» i l’elimina.
- Rebutjar una proposta `execute` mostra «Okay. I won't execute it.» i l’elimina.
- Una instrucció nova consumeix o substitueix la proposta anterior amb les traces
  existents, però mai l’executa implícitament.
- `/new` i `/clear` conserven les semàntiques actuals; només `/new` buida l’estat de
  sessió i les propostes.

### 6.6 `/plan` explícit

1. `/plan <objectiu>` continua creant un torn amb autoritat immutable plan-only.
2. El provider pot retornar `execute` com avui; el runtime mostra les ordres i no
   executa.
3. En acabar amb `planned`, el runtime crea una proposta `execute` a partir de
   l’objectiu resolt del workflow, no dels comandos generats.
4. La UI mostra la proposta com una continuació separada.
5. Una acceptació posterior inicia un workflow normal nou i torna a planificar.

Per tant, `/plan` continua sense cap camí al runner dins del torn original, però el
pla ja no és un carreró sense sortida conversacional.

## 7. Autoritat, confiança i privacitat

### 7.1 Invariants d’autoritat

- `action=plan` produeix zero execucions en qualsevol configuració.
- Una proposta `plan` o `execute` no és una confirmació de seguretat.
- L’objectiu o, com a mínim, un resum complet de la proposta és visible abans que una
  acceptació curta pugui resoldre-la.
- El provider pot demanar menys autoritat retornant `plan`, però mai augmentar-la.
- Acceptar `mode=plan` crea un torn plan-only; no crea un torn executable.
- Acceptar `mode=execute` crea un workflow nou, no reactiva el torn anterior.
- `/plan` continua tenint zero autoritat d’execució en tots els seus camins.
- `--yes-safe` només s’aplica dins del workflow executable posterior i després de la
  classificació local.

### 7.2 Autoritat en retries i continuacions bloquejades

L’autoritat plan-only s’ha de conservar més enllà del primer intent. La memòria curta
afegeix el mode del retry o intent pendent, sense persistència fora del procés.

- un error o cancel·lació d’un torn plan-only fa que `/retry` torni a ser plan-only;
- un `blocked/missing_input` produït mentre es prepara un pla manté plan-only per al
  següent follow-up;
- una nova instrucció rebuda mentre aquest context és pendent es tracta
  conservadorament com plan-only; un resultat terminal nou substitueix el context;
- cap retry d’un pla pot convertir-se silenciosament en execució perquè el flag es
  perdi entre torns.

La representació exacta pot ser un enum intern de mode o un booleà plan-only al costat
de l’objectiu pendent. No es reutilitza text del model com a font d’autoritat.

### 7.3 Confiança i dades

`PendingProposal` continua sent memòria efímera de sessió. S’hi afegeix només `Mode`.
No s’hi desen comandos, sortides, prompts ni classificacions de risc. Les traces
opt-in registren el mode i el cicle de vida, amb la mateixa advertència de dades
sensibles actual.

## 8. Errors i recuperació

- `action=plan` sense ordres o sense oferta executable és un error estructural i usa
  el pressupost finit de reparació existent.
- Una combinació invàlida d’oferta rep reparació estructural; mai es degrada a text
  lliure ni a execució.
- Si la preparació del pla necessita una dada que no es pot inferir ni obtenir sense
  executar, el model retorna `blocked/missing_input`. El mode plan-only es conserva.
- Si l’usuari cancel·la mentre es prepara el pla, l’objectiu i el mode queden
  disponibles per `/retry`.
- Si falla el workflow executable posterior, s’apliquen la recuperació, els intents i
  la causalitat existents; no es restaura automàticament el pla vell.
- Arribar al límit de planificació no promociona una proposta de pla a execució.

## 9. Interfície i traces

Es reutilitzen `Turn.Plan` i els renderers actuals. Només s’afegeixen invitacions i
missatges dependents del mode:

- oferta `plan`: «Would you like me to prepare an executable plan?»;
- oferta `execute`: «Would you like me to execute it?»;
- rebuig `plan`: «Okay. I won't prepare the plan.»;
- rebuig `execute`: «Okay. I won't execute it.».

Les traces existents incorporen `proposal_mode` a:

- `pending_proposal_created`;
- `pending_proposal_accepted`;
- `pending_proposal_declined`;
- `pending_proposal_replaced`.

`planner_result` i `shellia_decision` registren `action=plan`, i `turn_end` conserva
`outcome=planned`, nombre de plans i zero execucions. No s’afegeix una font de traces
ni persistència nova.

## 10. Àmbit i no-objectius

### 10.1 Dins d’abast

- `action=plan` i la seva matriu de validació;
- `offer.mode=plan|execute`;
- ofertes de pla des de respostes `answer` completes;
- promoció d’una proposta de pla a una proposta d’execució;
- acceptació, rebuig, substitució i retry amb mode preservat;
- flux de regressió complet basat en la conversa de memòria;
- prompt, schema, traces, README i documentació operativa afectada.

### 10.2 Fora d’abast

- executar directament el `CommandPlan` guardat d’un torn anterior;
- persistir propostes o plans després de tancar Shellia;
- afegir un classificador local complet de llenguatge natural;
- afegir un segon model o una crida separada de routing;
- garantir totes les formes d’acceptació en tots els idiomes;
- redissenyar els renderers o les confirmacions;
- canviar la classificació local de risc, `yes_safe`, repeticions o evidència causal;
- convertir `/plan` en un mode executable dins del mateix torn;
- permetre plans amb placeholders o passos purament conceptuals.

## 11. Slices de comportament

### Slice 1 — Contracte de pla i oferta tipada

- afegir `action=plan` i `offer.mode` al tipus, JSON Schema, parser compatible i
  validació;
- ampliar el prompt amb la distinció entre resposta, oferta, pla i execució;
- normalitzar els comandos d’un pla sense executar-los;
- proves RED/GREEN de la matriu completa i de respostes malformades.

Resultat independent: el runtime pot rebre i rebutjar correctament una decisió de pla,
però encara no conserva el cicle entre torns.

### Slice 2 — Resultat planificat i proposta d’execució

- rutear `action=plan` a `TurnOutcomePlanned` abans de qualsevol confirmació o
  executor;
- renderitzar amb la UI actual;
- publicar la proposta `execute` només després d’admetre el pla;
- afegir invitacions i traces tipades.

Resultat independent: una petició explícita de pla produeix ordres visibles, zero
execucions i una proposta executable pendent.

### Slice 3 — Continuació segura entre modes

- ampliar `PendingProposal` amb `Mode`;
- acceptar localment `plan` i `execute` amb autoritats diferents;
- promocionar `plan → execute` després d’un resultat planificat;
- preservar plan-only en `/retry`, cancel·lació i `blocked/missing_input`;
- cobrir rebuig, substitució i instrucció nova.

Resultat independent: el cicle complet funciona sense perdre autoritat ni executar
plans obsolets.

### Slice 4 — Regressió canònica i documentació

- afegir la conversa RAM com a prova de sessió determinista;
- actualitzar README, `docs/ai/project-overview.md`,
  `docs/ai/workflow-and-safety.md` i la memòria operativa afectada;
- verificar els quatre estils visuals només si els textos nous alteren fixtures.

## 12. Criteris d’acceptació

### 12.1 Pla explícit

- «fes un pla executable per alliberar RAM sense reiniciar» retorna almenys una ordre
  concreta i el seu propòsit;
- el resultat és `planned` i l’executor rep zero invocacions;
- una resposta textual sense ordres no és una decisió de pla vàlida;
- el pla crea una proposta `execute` visible i tipada.

### 12.2 Oferta des d’una resposta

- una resposta `answer` pot oferir preparar un pla, però no executar directament;
- l’oferta es desa només després d’un `complete` admès;
- «ok» després d’una oferta `plan` inicia un torn plan-only;
- «ok, quins passos són?» no pot iniciar execució per heurística local;
- una resposta que ja ha rebut la petició de pla no pot limitar-se a oferir el pla de
  nou.

### 12.3 Execució posterior

- «executa’l» amb una proposta `execute` inicia exactament l’objectiu ofert;
- el workflow torna a generar i mostrar el pla actual abans d’executar;
- un comando localment perillós conserva la confirmació, encara que el model el marqui
  com segur;
- sense proposta pendent, «executa’l» no recupera ni inventa ordres antigues;
- cap `CommandPlan` d’un torn anterior s’executa directament.

### 12.4 Recuperació i autoritat

- `/retry` després d’un error o cancel·lació plan-only continua sent plan-only;
- un follow-up després de `blocked/missing_input` durant un pla continua sense
  autoritat d’execució;
- rebutjar una proposta elimina només aquella proposta;
- una instrucció nova no executa la proposta substituïda;
- `/plan` manté zero invocacions de l’executor en totes les respostes i pot deixar una
  proposta d’execució separada només després de mostrar un pla vàlid;
- el resum de qualsevol proposta és visible abans que `sí`, `ok` o un equivalent
  pugui acceptar-la.

### 12.5 Regressió canònica

Una prova de sessió amb fake LLM i `runtimeDeps` cobreix:

1. observació inicial de RAM i swap;
2. resposta a «hi ha manera d’alliberar RAM sense reiniciar?» amb oferta `plan`;
3. «ok, quins passos són?»;
4. decisió `action=plan` amb ordres concretes i zero execucions;
5. proposta `execute` pendent;
6. «executa’l»;
7. replanificació actual, classificació local i confirmació normal;
8. absència de respostes repetitives del tipus «si vols, et puc donar els passos».

La prova valida les peticions enviades al fake provider, els resultats, les invocacions
de l’executor i els events de traça. No intenta provar comprensió lingüística d’un
model real dins de CI.

## 13. Verificació

La implementació seguirà RED/GREEN als límits estables:

- `internal/llm`: taula de schema/parser per `action=plan` i `offer.mode`;
- `internal/app/workflow`: matriu acció/operació i invariant de zero execució;
- `internal/session`: resolució, rebuig, promoció i preservació de mode;
- `internal/app`: proves de sessió completes amb executor sentinella;
- `internal/ui`: comprovacions focalitzades dels dos textos canònics si afecten
  renderitzat.

Tancament obligatori:

```text
gofmt -w ./cmd ./internal
env GOCACHE=/tmp/go-build go test -count=1 ./...
go vet ./...
go build -o shellia ./cmd/shellia
env GOCACHE=/tmp/go-build go test -race -count=1 ./internal/app ./internal/session ./internal/llm
git diff --check
```

Cal una revisió independent centrada en autoritat, retries plan-only, confirmacions i
execució de dades obsoletes abans de considerar la feature completa.

## 14. Stop gates

La implementació s’atura i torna a disseny si:

1. `action=plan` pot arribar a l’executor, directament o durant una reparació;
2. acceptar una proposta `plan` crea un workflow executable;
3. el provider pot seleccionar `offer.mode=execute` en una combinació no admesa i
   obtenir autoritat;
4. un retry o follow-up d’un pla perd plan-only i pot executar silenciosament;
5. cal desar o executar ordres d’un torn anterior per completar el flux;
6. la classificació local o les confirmacions es rebaixen per haver vist el pla abans;
7. una acceptació ambigua pot activar execució mitjançant una heurística local;
8. conviuen dos contractes funcionals de resposta o dos camins d’execució;
9. la conversa canònica continua podent acabar amb un `answer/complete` genèric quan
   el model ha reconegut que el lliurable demanat és un pla;
10. `/plan` adquireix autoritat d’execució dins del mateix torn.

## 15. Riscos i decisions obertes

| Risc | Mitigació decidida |
| --- | --- |
| El model classifica malament una petició de pla | Contracte explícit, exemples al prompt i regressió canònica; no s’afegeix un classificador local fràgil. |
| El pla i el pla final d’execució difereixen | Es torna a mostrar el pla actual; l’usuari confirma l’acció real, no una còpia obsoleta. |
| Acceptacions multilingües incompletes | Es mantenen només acceptacions locals inequívoces; seguiments rics passen pel model sense autoritat automàtica. |
| Un pla bloquejat deriva en execució al follow-up | El mode plan-only forma part de l’estat pendent i del retry. |
| Les ofertes proliferen al final de cada resposta | `answer` només pot oferir un pla adjacent concret i no pot oferir el lliurable ja demanat. |
| Canvi del wire schema afecta perfils strictes | Schema estricte, parser compatible i tests de tots dos modes canvien en el mateix slice. |

No queda cap decisió de negoci pendent per implementar aquesta especificació. La
representació interna exacta del mode de retry és una decisió local sempre que
preservi els invariants i criteris anteriors.

## 16. Documentació i traspàs

S’actualitzaran:

- `README.md`, amb un exemple proposta → pla → execució;
- `docs/ai/project-overview.md`, amb `action=plan` i propostes tipades;
- `docs/ai/workflow-and-safety.md`, amb els nous límits d’autoritat;
- la memòria operativa del workflow quan la implementació quedi verificada.

La feature necessita quatre slices ordenats perquè canvia el wire contract, el
runtime i l’estat de sessió amb checkpoints d’autoritat. El següent artefacte és
`design-project-plan`; després, la implementació correspon a
`implement-project-feature`.
