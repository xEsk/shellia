# Disseny d’intenció i finalització causal per a l’agent de terminal

**Data:** 2026-08-07

**Estat:** pendent de revisió

**Nivell:** CRITICAL

**Àmbit:** interpretació de la petició, autoritat d’execució, consultes de capacitat, finalització i continuïtat de sessió

## 1. Resum

Shellia mantindrà un biaix proactiu propi d’un agent executor de terminal, però distingirà quatre intencions canòniques:

- `act`: produir un canvi al sistema;
- `observe`: consultar el sistema per obtenir una dada o estat;
- `capability`: explicar si Shellia pot fer una acció, com l’abordaria i oferir-ne l’execució;
- `explain`: respondre o explicar sense executar.

Una consulta explícita de capacitat, incloses formes com «pots fer X?» o «pots mirar X?», no autoritza l’execució. Shellia respon si és capaç de fer-ho, explica breument el procediment i, quan és viable, acaba oferint executar-lo. Una acceptació posterior reprèn una proposta estructurada i entra al workflow executable normal.

Les peticions `act` i `observe` són proactives: Shellia no inventa una confirmació conversacional abans de proposar comandos. La classificació local, la visibilitat dels comandos i les confirmacions de seguretat existents continuen sent l’autoritat final.

La intenció també governa quan es pot acceptar `complete`. Saber com executar una acció no demostra que l’acció s’hagi completat. Els objectius de canvi o observació necessiten evidència causal adequada abans de finalitzar.

## 2. Problema i valor

El workflow orientat a objectius actual diferencia `execute`, `complete` i `blocked`, però deixa al model dues decisions que el runtime gairebé no valida semànticament:

1. si el missatge demana actuar, observar, explicar o només consultar una capacitat;
2. si el text lliure de `completion_basis` demostra realment que l’objectiu actual està resolt.

Això permet dos errors visibles:

- Shellia pot demanar permís conversacional per fer una descoberta segura en lloc de proposar els comandos i deixar que la política local decideixi;
- Shellia pot respondre com s’hauria de fer una acció i declarar-la completada sense haver-la executat.

Després d’un fals `complete`, la sessió neteja l’objectiu pendent. Seguiments com «és el que volia» o «per què no ho fas?» queden reduïts a text ambigu i el model pot repetir la mateixa explicació.

El valor d’aquesta feature és fer Shellia proactiu sense confondre una pregunta de capacitat amb autoritat per actuar, i impedir finalitzacions que no estiguin causades pel workflow actual.

## 3. Resultat observable

| Entrada | Intenció | Comportament |
| --- | --- | --- |
| `Actualitza Codex` | `act` | Descobrir el mètode si cal, mostrar el pla, confirmar segons la política actual, executar i avaluar el resultat. |
| `Necessito actualitzar Codex` | `act` | Tractar la necessitat com un objectiu executable, no com una invitació a explicar. |
| `Quant espai queda al disc?` | `observe` | Executar la consulta mínima i respondre amb evidència actual. |
| `Mira quant espai queda al disc` | `observe` | Executar la consulta mínima i respondre. |
| `Pots mirar quant espai queda al disc?` | `capability` | No executar. Explicar que pot usar `df`, indicar que és lectura i oferir l’execució. |
| `Pots actualitzar Codex?` | `capability` | No executar. Explicar el procediment i els efectes, i oferir l’execució. |
| `Com actualitzaries Codex?` | `explain` | Explicar sense executar ni crear una oferta pendent obligatòria. |
| `/plan actualitza Codex` | `act` amb autoritat plan-only | Mostrar el pla sense executar, com ara. |

La forma interrogativa no determina tota sola el comportament. Una pregunta que demana una dada actual és `observe`; una pregunta explícita sobre allò que Shellia pot fer és `capability`.

## 4. Invariants no negociables

### 4.1 Autoritat

- `capability` i `explain` no poden invocar l’executor en el torn actual.
- `act` i `observe` poden arribar a l’executor només quan l’autoritat immutable del torn ho permet.
- `/plan` continua tenint `executionAllowed=false` independentment de la intenció.
- Una oferta conversacional no és una confirmació de seguretat.
- Acceptar una oferta inicia un nou workflow executable que torna a mostrar el pla i aplica totes les confirmacions normals.
- El model no pot reduir risc, saltar confirmacions ni convertir una consulta de capacitat en autoritat d’execució.

### 4.2 Interpretació proactiva

- Davant del dubte entre `act` i `explain`, una petició orientada a obtenir un resultat es tracta com `act`.
- Davant del dubte entre `observe` i una resposta de coneixement, una dada local o mutable es tracta com `observe` i s’obté amb eines.
- Una formulació explícita de capacitat —«pots...», «ets capaç de...», «seria possible que Shellia...» o equivalent semàntic— es tracta com `capability`, encara que pugui ser una fórmula amable.
- `act` i `observe` no poden acabar en `blocked/missing_input` només per demanar permís per usar comandos. La política local ja governa aquesta autoritat.
- L’entrada actual preval sobre explicacions i observacions històriques.

### 4.3 Finalització causal

- `complete` per a `explain` pot basar-se en coneixement directe.
- `complete` per a `capability` resol la pregunta de capacitat, però no afirma que l’acció oferta s’hagi realitzat.
- `complete` per a `observe` requereix una observació actual o evidència reutilitzable que sigui explícitament prou fresca per a la pregunta.
- `complete` per a `act` requereix una execució reeixida del workflow actual o una observació actual que demostri que la postcondició ja es compleix.
- Conèixer el comando, el mètode d’instal·lació o els passos necessaris no és evidència de completar `act`.
- Un `complete` incompatible amb la intenció es rebutja abans de netejar estat de sessió o renderitzar èxit.

## 5. Fora d’abast

- Canviar la classificació local de seguretat o els textos de confirmació existents.
- Executar automàticament una consulta explícita de capacitat.
- Afegir un segon model, una segona crida de routing o un classificador local complet de llenguatge natural.
- Garantir comprensió perfecta de qualsevol idioma mitjançant llistes exhaustives de frases.
- Persistir propostes després de tancar el procés.
- Redissenyar la UI o afegir un diàleg modal nou.
- Verificar sempre una mutació amb un comando addicional quan l’execució mateixa ja és evidència suficient.

## 6. Reutilització i propietaris

La feature amplia els propietaris actuals:

- `internal/llm`: declara la intenció, els criteris d’èxit, la base causal i l’oferta opcional dins de la decisió existent;
- `internal/app`: bloqueja la intenció durant el workflow, valida `complete`, governa reparacions i impedeix executar `capability` o `explain`;
- `internal/session`: conserva una proposta pendent estructurada i resol acceptacions o rebuigs inequívocs;
- `internal/executor` i `internal/safety`: es reutilitzen sense canviar la seva política;
- `internal/ui`: reutilitza el renderer final i afegeix una invitació canònica quan hi ha una oferta viable;
- `internal/trace`: registra intenció, criteris d’èxit, validació causal i cicle de vida de l’oferta.

No es crea un router separat. La primera decisió de planificació continua sent una sola crida i el workflow continua tenint un únic propietari a `internal/app`.

## 7. Contracte estructurat

La resposta de planificació conserva `action=execute|complete|blocked` i afegeix:

```text
objective_mode: act | observe | capability | explain
success_criteria: descripció breu del resultat que tanca l’objectiu
completion_basis:
  type: model_knowledge | current_observation | current_execution | prior_session_evidence
  evidence_revision: número opcional
  attempt_ids: llista acotada opcional
offer:
  objective: objectiu executable ofert, opcional
  summary: descripció breu, opcional
```

### 7.1 Bloqueig del contracte

La primera resposta estructuralment vàlida fixa `objective_mode` i `success_criteria` durant el torn. Les rondes posteriors els reben com a contracte immutable i no els poden reinterpretar per justificar una finalització més fàcil.

Si la primera resposta és internament contradictòria —per exemple, `objective_mode=act`, `action=complete` i base `model_knowledge`— el runtime no bloqueja encara un contracte incorrecte: demana una reparació semàntica amb la contradicció concreta. Només una decisió coherent fixa el contracte.

### 7.2 Ofertes de capacitat

Per a `objective_mode=capability`:

- `action` és `complete` perquè la pregunta de capacitat queda resposta;
- no hi ha comandos executables en el torn;
- si Shellia és capaç d’oferir l’acció, `offer.objective` és obligatori;
- la UI mostra la resposta i afegeix la pregunta canònica «Vols que ho executi?»;
- si Shellia no té la capacitat o no pot actuar amb seguretat, l’oferta queda buida i la resposta explica el límit sense invitació falsa.

L’oferta conserva un objectiu, no un comando autoritzat. Quan l’usuari l’accepta, el nou workflow torna a planificar amb el context actual. Això evita executar una proposta textual obsoleta o extreta de Markdown.

### 7.3 Bases de finalització

La validació mínima és:

| Intenció | Bases admissibles |
| --- | --- |
| `explain` | `model_knowledge`, `prior_session_evidence`, `current_observation`, `current_execution` |
| `capability` | `model_knowledge`, `prior_session_evidence`, `current_observation` |
| `observe` | `current_observation`; `prior_session_evidence` només quan la frescor és explícitament suficient |
| `act` | `current_execution` o `current_observation` que demostri la postcondició actual |

Quan una base requereix evidència actual, `evidence_revision` o `attempt_ids` han de referenciar evidència real del workflow. El runtime valida existència i causalitat, no intenta entendre semànticament tota la sortida del comando.

## 8. Fluxos

### 8.1 Instrucció executable

1. El model classifica `act` o `observe` i defineix el criteri d’èxit.
2. Si calen eines, retorna `execute`.
3. Shellia mostra el pla i aplica la seguretat actual.
4. L’executor produeix intents i evidència.
5. El model torna a decidir amb el contracte bloquejat.
6. `complete` només s’accepta si la base causal és compatible.

### 8.2 Consulta de capacitat

1. El model classifica `capability`.
2. Shellia valida que no hi hagi comandos executables.
3. Es respon capacitat, procediment i efectes rellevants.
4. Si és viable, es desa `PendingProposal{Objective, Summary}` i es mostra «Vols que ho executi?».
5. El torn acaba sense invocar l’executor.

### 8.3 Acceptació posterior

1. Amb una proposta pendent, una acceptació inequívoca com `sí`, `yes`, `fes-ho`, `endavant` o `executa-ho` es resol localment contra l’objectiu ofert.
2. El text original de l’usuari es conserva a traces i historial; el prompt rep també l’objectiu executable resolt.
3. S’inicia un workflow normal amb `act` o `observe`, segons l’objectiu replantejat.
4. La proposta es consumeix quan el workflow s’inicia; si el torn es cancel·la o falla abans d’executar, l’objectiu queda disponible mitjançant la memòria de retry existent.

Una negativa inequívoca elimina la proposta. Una instrucció nova i no relacionada la substitueix. Una resposta ambigua no executa per heurística local: es deixa al model amb la proposta visible, però continua subjecta als invariants d’autoritat.

## 9. Errors i reparació

- Una intenció desconeguda és un error estructural.
- `capability` o `explain` amb `action=execute` és una contradicció i no arriba a l’executor.
- `act` completat només amb coneixement rep una reparació semàntica: el model ha d’executar, observar una postcondició o bloquejar-se per una causa real.
- `observe` completat amb evidència antiga quan la pregunta demana estat actual rep la mateixa reparació.
- Només es permet una reparació semàntica per torn, compartida amb el pressupost finit del workflow però separada de la reparació de JSON mal format.
- Si la contradicció persisteix, el torn acaba en `structural_error` o `no_progress` sense presentar l’objectiu com a completat.
- Cap reparació modifica `executionAllowed` ni evita confirmacions.

## 10. Memòria, confiança i privacitat

`SessionState` afegeix una proposta pendent compacta:

```text
PendingProposal
  objective
  summary
```

No s’hi persisteixen comandos autoritzats, prompts complets ni sortides noves. La proposta existeix només durant la sessió interactiva actual, com la resta de memòria curta.

La sortida dels comandos continua sent evidència no fiable. Ni una observació anterior ni una oferta del model obtenen autoritat d’execució. L’acceptació de l’usuari crea l’objectiu; la política local continua decidint si cada comando pot executar-se i amb quina confirmació.

## 11. Traces

S’afegeixen, dins de la traça opt-in actual:

- `objective_contract`: mode i criteri d’èxit bloquejats;
- `completion_validation`: base proposada, referències causals i resultat d’admissió;
- `pending_proposal_created`, `pending_proposal_accepted`, `pending_proposal_declined` i `pending_proposal_replaced`;
- motiu de qualsevol reparació semàntica.

No s’amplia la política de persistència ni el contingut sensible capturat per defecte.

## 12. Criteris d’acceptació

### 12.1 Intenció

- `Quant espai queda al disc?` pot executar una consulta de lectura i respondre amb evidència.
- `Pots mirar quant espai queda al disc?` executa zero comandos, explica el procediment i ofereix executar-lo.
- `Actualitza Codex` no pot acabar només explicant el comando d’actualització.
- `Com actualitzaries Codex?` executa zero comandos i no converteix automàticament l’explicació en una acció pendent.
- Una petició `act` o `observe` no demana permís conversacional per proposar comandos.

### 12.2 Oferta i follow-up

- Una oferta viable queda estructurada a la sessió sense dependre de backticks.
- `sí` després d’una oferta inicia exactament l’objectiu ofert.
- L’inici del nou workflow no evita el pla visible ni cap confirmació de seguretat.
- Una negativa elimina l’oferta sense executar.
- Una instrucció nova substitueix l’oferta anterior.
- Sense oferta pendent, un `sí` no inventa cap acció executable.

### 12.3 Finalització causal

- `act + complete + model_knowledge` és rebutjat i reparat una vegada.
- `act` pot completar-se després d’una execució actual reeixida referenciada.
- `observe` que demana estat actual no pot completar-se només amb una observació antiga.
- `capability` pot completar la pregunta sense confondre l’oferta amb una acció realitzada.
- Una contradicció persistent acaba sense fals èxit.

### 12.4 Seguretat i modes

- `capability` i `explain` produeixen zero invocacions de l’executor.
- `/plan` produeix zero invocacions de l’executor per a les quatre intencions.
- Una oferta acceptada que deriva en un comando perillós conserva la confirmació actual.
- El risc local mai disminueix per la intenció, la base de finalització o l’oferta.

### 12.5 Regressió canònica

Una prova de sessió cobreix la seqüència:

1. descobrir com està instal·lat Codex;
2. `actualitza codex`;
3. impedir un `complete` basat només en la descoberta anterior;
4. generar el pla d’actualització i passar per la confirmació normal;
5. no repetir indefinidament l’explicació en follow-ups.

Una segona prova diferencia `Quant espai queda?` de `Pots mirar quant espai queda?`.

## 13. Verificació i stop gates

La implementació és acceptable quan:

- les transicions i validacions pures tenen proves RED/GREEN;
- les proves del prompt cobreixen les quatre intencions i contradiccions;
- les proves de sessió cobreixen oferta, acceptació, rebuig i substitució;
- les proves del bucle demostren zero execució per `capability`, `explain` i `/plan`;
- la suite completa, `go vet`, build i detector de races dels paquets afectats passen;
- una revisió independent no detecta regressions d’autoritat, seguretat o falsos èxits;
- no queda cap heurística viva basada en extreure comandos suggerits del Markdown quan hi ha una proposta estructurada.

Stop gates:

- si el contracte no pot distingir una oferta d’una autorització sense afegir un segon motor, s’ha de tornar al disseny;
- si validar `complete` exigeix interpretar semànticament sortides arbitràries dins del runtime, la validació s’ha de limitar a causalitat i referències reals, no crear un verificador local fràgil;
- si la proposta pendent pot saltar la classificació local o executar un comando preautoritzat, la implementació no és admissible.

## 14. Documentació

S’actualitzarà el README amb exemples breus de:

- diferència entre una observació i una consulta de capacitat;
- acceptació d’una oferta en el torn següent;
- preservació de `/plan` i de les confirmacions de seguretat.

No s’afegeixen flags ni configuració nova.
