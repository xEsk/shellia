# Disseny de context de sessió referenciable i contracte d’evidència

**Data:** 2026-08-11

**Estat:** aprovat, pendent d’implementació

**Nivell:** CRITICAL

**Àmbit:** interpretació de follow-ups, autoritat d’execució, procedència i frescor de l’evidència, recuperació de resultats de sessió, processament per fragments i observacions compactes

## 1. Resum

Shellia separarà quatre dimensions que el contracte actual barreja dins d’`objective_mode`:

- l’operació que resol la petició;
- la font d’informació necessària;
- la frescor exigida;
- l’autoritat d’execució, que continuarà sent propietat immutable del runtime.

Una petició com «reformata la resposta anterior» es resoldrà a partir d’un resultat històric identificat i no podrà arribar a l’executor. Una petició com «comprova els ports ara» continuarà requerint una observació actual. El runtime validarà combinacions estructurals; no incorporarà llistes de frases, verbs o idiomes per decidir el comportament.

Els vuit resultats que la sessió interactiva ja conserva rebran identificadors estables. El planner podrà demanar-ne qualsevol mitjançant una nova decisió `retrieve_context`, que no és una execució de terminal. Els resultats grans es processaran per fragments amb una comprovació de completesa abans de permetre `complete`.

El prompt de planificació també coneixerà el pressupost d’evidència abans de la primera ordre i demanarà sortides filtrades, agregades i deduplicades quan una observació pugui ser extensa. Una sortida truncada podrà originar un únic refinament estructurat per reduir-la, evitant successions de descobertes semànticament equivalents.

## 2. Problema i evidència

El contracte implementat a `docs/superpowers/specs/2026-08-07-intent-aware-terminal-agent-design.md` protegeix correctament les consultes d’estat actual contra evidència obsoleta, però no representa una transformació d’un resultat de sessió.

En la reproducció canònica:

1. l’usuari demana els ports oberts;
2. Shellia obté i presenta la llista completa;
3. l’usuari demana reformatejar aquella resposta sense repetir la descoberta;
4. el model produeix correctament la llista Markdown sense comandos;
5. el runtime rebutja `complete` perquè la base cita una revisió d’un workflow anterior;
6. la reparació d’`observe` obliga el model a refrescar l’estat i Shellia executa de nou;
7. la nova sortida extensa substitueix observacions compactes, mentre l’historial projectat al prompt només conserva una previsualització truncada;
8. els follow-ups posteriors afirmen erròniament que la resposta completa ja no està disponible.

El primer límit trencat és el contracte de procedència i frescor, no la captura del comando: el resultat complet encara existia a l’historial de la sessió quan el runtime va rebutjar la primera resposta correcta.

La mateixa sessió va executar vuit comandos per a la descoberta inicial. El model va reduir progressivament la sortida amb `-Fn`, `awk` i `sort -u` perquè només rebia 1.200 caràcters d’evidència entre rondes. Com que els comandos eren textualment diferents, la protecció de repeticions no els va considerar duplicats.

## 3. Resultat observable

| Entrada | Operació | Font | Frescor | Comportament |
| --- | --- | --- | --- | --- |
| `Quins ports estan oberts ara?` | `observe` | `current_observation` | `current` | Executar la consulta mínima i respondre amb evidència actual. |
| `Reformata la resposta anterior` | `transform` | `session_result` | `snapshot` | Recuperar el resultat citat i executar zero comandos. |
| `Resumeix el primer resultat` | `answer` | `session_result` | `snapshot` | Recuperar aquell resultat per ID i resumir-lo. |
| `Compara el primer resultat amb l’últim` | `answer` | `session_result` | `snapshot` | Recuperar els dos resultats i comparar-los. |
| `Torna a comprovar els ports` | `observe` | `current_observation` | `current` | Fer una observació nova; no reutilitzar el snapshot com a estat actual. |
| `Què em vas dir abans?` | `answer` | `session_result` | `snapshot` | Respondre des de l’historial, sense executor. |
| `Pots comprovar els ports?` | `capability` | `model_knowledge` | `not_applicable` | Explicar la capacitat i oferir-la sense executar. |

Una transformació o resposta basada en historial pot descriure què es va dir o observar abans, però no demostrar què és cert ara.

## 4. Objectius i fora d’abast

### 4.1 Objectius

- Permetre referenciar qualsevol dels vuit resultats actuals de la sessió.
- Preservar el contingut complet necessari per a transformacions i comparacions.
- Separar operació, font, frescor i autoritat.
- Impedir que la recuperació de context arribi a l’executor.
- Processar resultats grans per fragments sense omissió silenciosa.
- Conservar `/retry`, `/plan`, la seguretat local i les confirmacions existents.
- Reduir observacions redundants mitjançant instruccions generals i un refinament acotat.

### 4.2 Fora d’abast

- Persistir historial després de tancar Shellia.
- Compartir historial entre processos o sessions.
- Fer que `/new` conservi resultats anteriors.
- Afegir una base de dades, fitxers d’historial o un servei nou.
- Afegir un router LLM separat o una crida de classificació prèvia.
- Detectar intencions amb regex, paraules clau o llistes dependents de l’idioma.
- Interpretar semànticament sortides arbitràries dins del runtime.
- Garantir localment que una traducció o un resum del model sigui lingüísticament perfecte.
- Inferir equivalència semàntica general entre comandos diferents.
- Canviar la classificació de risc, `yes_safe`, els timeouts o les confirmacions.

## 5. Contracte de decisió

La resposta de planificació mantindrà un únic contracte i ampliarà les dimensions actuals:

```text
action:
  execute | retrieve_context | complete | blocked

operation:
  answer | transform | observe | act | capability

evidence_source:
  model_knowledge | session_result | retry_observation |
  current_observation | current_execution

freshness:
  not_applicable | snapshot | current

success_criteria:
  descripció concreta del resultat que tanca l’objectiu

context_refs:
  llista d’identificadors de resultats de sessió

completion_basis:
  source
  freshness
  context_revision opcional
  evidence_revision opcional
  attempt_ids opcionals
```

### 5.1 Significat de les operacions

- `answer`: respondre, sintetitzar o comparar sense necessitat de preservar substancialment la forma del text original;
- `transform`: reformatejar, traduir o reorganitzar contingut de sessió preservant-ne substancialment la informació;
- `observe`: obtenir una dada o estat local actual;
- `act`: produir un canvi al sistema;
- `capability`: explicar si Shellia pot realitzar una operació i, si és viable, oferir-la.

Els noms són categories del protocol, no paraules que el runtime busqui dins del missatge. La classificació semàntica continua formant part de la decisió del model.

### 5.2 Autoritat immutable

El runtime continua derivant `executionAllowed` del mode real d’entrada. El model no retorna ni modifica aquest valor.

- `/plan` manté `executionAllowed=false`.
- `retrieve_context` no pot invocar l’executor independentment del mode del torn.
- `answer`, `transform` i `capability` no admeten `action=execute`.
- `observe` i `act` només poden arribar a l’executor quan `executionAllowed=true`.
- Cap resultat històric, fragment, oferta o text recuperat pot habilitar execució.

### 5.3 Bloqueig del contracte

El workflow només bloqueja `operation`, `evidence_source`, `freshness` i `success_criteria` després d’una decisió coherent.

Una primera resposta contradictòria, com `transform + current_observation + execute`, rep una reparació semàntica abans de fixar el contracte. La reparació pot corregir dimensions dins del mateix grup d’autoritat, però mai convertir una operació no executable en autoritat de terminal.

## 6. Matriu de validació

| Operació | Font | Frescor | Accions admissibles |
| --- | --- | --- | --- |
| `answer` | `model_knowledge` | `not_applicable` | `complete`, `blocked` |
| `answer` | `session_result` | `snapshot` | `retrieve_context`, `complete`, `blocked` |
| `transform` | `session_result` | `snapshot` | `retrieve_context`, `complete`, `blocked` |
| `observe` | `current_observation` | `current` | `execute`, `complete`, `blocked` |
| `observe` | `retry_observation` | `current` | `complete`, `execute`, `blocked`, només per al retry elegible |
| `act` | `current_execution` | `current` | `execute`, `complete`, `blocked` |
| `act` | `current_observation` | `current` | `execute`, `complete`, `blocked` quan demostra la postcondició |
| `capability` | `model_knowledge` | `not_applicable` | `complete`, `blocked` |

Regles addicionals:

- `complete + session_result` requereix una `context_revision` completa que contingui tots els `context_refs` necessaris;
- `complete + current_observation/current_execution` conserva les validacions d’`evidence_revision` i `attempt_ids` actuals;
- `retry_observation` conserva el lligam estricte amb `LastRetryInstruction` i `LastObservationObjective`;
- un resultat de sessió amb outcome bloquejat, fallit o declinat es pot citar com a text, però no com a prova de completar `observe` o `act`;
- una font històrica no és compatible amb `freshness=current`, excepte `retry_observation` quan el runtime valida el retry exacte;
- `complete` no pot contenir comandos ni peticions de recuperació pendents.

## 7. Catàleg de resultats de sessió

La sessió interactiva ampliarà l’entrada d’historial existent:

```text
SessionResult
  id
  instruction
  outcome
  result
  character_count
```

### 7.1 Identitat i retenció

- La sessió conserva un màxim de vuit resultats, reutilitzant `maxHistoryEntries`.
- Els IDs són monotònics durant la vida del procés, per exemple `result-1`, `result-2`.
- Expulsar l’entrada més antiga no renumera ni reutilitza IDs vius.
- `/new` buida el catàleg però no reinicia el comptador; cap resultat nou pot reutilitzar un ID invalidat dins del mateix procés.
- Tancar Shellia elimina el catàleg i el comptador.
- No s’afegeix persistència ni configuració paral·lela de retenció.

Qualsevol dels vuit outcomes conservats és referenciable com a text. La validesa com a evidència causal depèn de la matriu anterior.

### 7.2 Catàleg projectat al prompt

La primera ronda rep un catàleg acotat, no els resultats complets:

```text
id
instruction
outcome
character_count
preview
```

La previsualització serveix per seleccionar la referència, però no és evidència suficient per completar una transformació o resposta basada en aquell contingut. El runtime exigeix `retrieve_context` abans d’acceptar `complete + session_result`.

## 8. Recuperació de context

### 8.1 Flux

1. El planner rep l’objectiu i el catàleg de resultats.
2. Retorna `action=retrieve_context` i una llista `context_refs` no buida.
3. `internal/app` valida que tots els IDs existeixen i encara són vius.
4. El workflow crea una `context_revision` en estat pendent.
5. El runtime carrega el contingut complet o l’envia al processador de fragments.
6. Quan totes les referències i fragments estan processats, la revisió passa a completa.
7. El mateix planner rep l’objectiu, el contracte immutable i la revisió completa.
8. `complete` només s’admet si cita aquella revisió i no en falta cap referència.

`retrieve_context` no crea un pla de terminal, no passa per `internal/safety`, no demana confirmació i no genera un intent de comando.

### 8.2 Resultats petits

Quan tots els resultats seleccionats caben dins del pressupost intern de context, la següent ronda els rep complets i delimitats. Com que la resolució necessita més d’una crida al model, la UI mostra una única línia d’estat:

```text
Processant 1 resultat de la sessió…
```

La mida del pressupost i dels fragments es manté com una política interna testable. Aquesta feature no afegeix configuració pública fins que hi hagi una necessitat observable de producte.

## 9. Processament per fragments

### 9.1 Divisió

Quan el contingut seleccionat no cap al pressupost dedicat, el runtime el divideix:

- sempre en límits UTF-8 vàlids;
- preferentment en límits de paràgraf o línia;
- conservant l’ordre exacte;
- sense solapaments que puguin duplicar contingut;
- amb metadades `result_id`, `fragment_index` i `fragment_count`.

La UI mostra una sola línia, no una per fragment:

```text
Processant 6 fragments de 2 resultats de la sessió…
```

### 9.2 Transformacions

Per a `operation=transform`:

1. cada fragment es processa amb l’objectiu i el contracte immutable;
2. la resposta estructurada reconeix explícitament l’ID del fragment consumit;
3. els resultats parcials es conserven en ordre;
4. una consolidació final resol estructures que travessen fronteres de fragment;
5. la revisió només es completa quan el conjunt de fragments reconeguts coincideix exactament amb el conjunt esperat.

### 9.3 Respostes, síntesis i comparacions

Per a `operation=answer`:

1. cada fragment produeix notes estrictament relacionades amb els criteris d’èxit;
2. les notes conserven la procedència `result_id/fragment_index`;
3. la reducció final rep totes les notes i les referències seleccionades;
4. una comparació de diversos resultats conserva la identitat de cada font.

### 9.4 Límits i completesa

- El nombre de fragments i crides internes és finit.
- Una resposta estructuralment invàlida pot rebre una reparació acotada per fragment.
- Un fragment absent, consolidat més d’una vegada, cancel·lat o fallit impedeix completar la revisió. Un retry HTTP pot reenviar-lo, però només una resposta estructuralment admesa pot incorporar-lo a la revisió.
- Si els intermedis o la consolidació superen els límits interns, el torn acaba bloquejat amb una explicació explícita.
- No es renderitza cap transformació parcial com a èxit.
- No es presenta mai contingut truncat com si fos complet.

La comprovació garanteix participació i procedència de tots els fragments. No pretén demostrar localment la perfecció semàntica del text generat pel model.

## 10. Prompt per a observacions compactes

La primera ronda de planificació rep sempre el pressupost d’evidència de comandos, encara que encara no hi hagi observacions.

El prompt estable afegeix regles generals:

- demanar només els camps necessaris per als criteris d’èxit;
- filtrar, agregar i deduplicar a l’origen quan una sortida pugui superar el pressupost;
- evitar una consulta àmplia seguida de consultes que només en reformategen la mateixa informació;
- permetre pipelines de lectura quan siguin necessàries per acotar l’evidència;
- completar sense executar quan l’evidència actual ja conté el valor exacte;
- davant d’una sortida truncada, proposar com a màxim un refinament orientat a reduir-la.

Aquestes regles no esmenten `lsof`, ports, plataformes ni comandos específics.

### 10.1 Refinament estructurat

El pla de comando amplia les metadades amb:

```text
refinement_reason:
  "" | reduce_output

refines_attempt_ids:
  llista d’intents previs
```

`reduce_output` només és admissible quan:

- referencia un intent anterior amb sortida marcada com a truncada;
- manté el mateix objectiu d’evidència;
- redueix camps, files o duplicats en lloc d’ampliar la descoberta;
- no existeix ja un refinament `reduce_output` admès per aquell intent arrel.

El runtime valida les referències i el nombre de refinaments. No intenta provar equivalència semàntica arbitrària entre comandos. Si el refinament també és insuficient, el model ha de completar amb el que és demostrable o acabar bloquejat; no pot iniciar una cadena indefinida de reformateigs executables.

## 11. Errors i recuperació

### 11.1 Referències d’historial

- ID desconegut o expulsat: `blocked/missing_input`, sense executor;
- catàleg buit: el model no pot inventar una referència;
- `/new`: invalida totes les referències anteriors i no en reutilitza els IDs durant la vida del procés;
- outcome no reeixit: continua disponible com a text, amb la seva naturalesa explícita.

### 11.2 Processament intern

- Error HTTP: reutilitza retries, límits i cancel·lació del client actual;
- error estructural: una reparació acotada per fragment;
- error semàntic persistent: bloqueig intern, no fals èxit;
- cancel·lació: descarta acumuladors i revisions pendents;
- timeout: no reprèn automàticament el fragment fora del contracte de retry;
- límit de fragments o pressupost: bloqueig explícit amb la referència afectada.

Una cancel·lació amb processament parcial conserva l’objectiu per a `/retry`, però no publica una revisió incompleta com a evidència reutilitzable.

### 11.3 Reparació semàntica

Exemples:

- `transform + current_observation + execute` es repara cap a `session_result + snapshot + retrieve_context`;
- `observe + session_result + current` es repara cap a una observació actual;
- `complete + session_result` sense revisió carregada es repara cap a `retrieve_context`;
- `retrieve_context` amb referències inexistents acaba bloquejat, no executa una descoberta substitutiva.

## 12. Confiança, privacitat i traces

Els resultats recuperats i fragments són dades no fiables:

- es delimiten fora de les instruccions del sistema;
- no poden canviar l’objectiu, l’operació, la frescor o l’autoritat;
- no poden injectar comandos ni marcar intents com a executats;
- les instruccions contingudes dins d’un resultat històric no són autoritat nova.

La feature no persisteix contingut per defecte. Quan les traces opt-in estan activades, els fragments poden aparèixer dins dels prompts complets, igual que qualsevol altre contingut enviat al model. Aquesta conseqüència s’ha de documentar.

S’afegeixen els esdeveniments:

- `context_retrieval_requested`;
- `context_fragment_start`;
- `context_fragment_complete`;
- `context_revision`;
- `context_consolidation`.

Les traces registren IDs, índexs, estat i causa de fallada. No s’afegeix una política de persistència nova.

## 13. Propietaris i migració

### 13.1 Propietaris

- `internal/app`: workflow, autoritat, catàleg viu, revisions de context, fragmentació, consolidació i admissió de refinaments;
- `internal/llm`: contractes JSON, prompts de planificació i processament, validació estructural i client existent;
- `internal/session`: projecció de memòria curta, elegibilitat de retry i tipus de resultat referenciable;
- `internal/ui`: estat breu de processament;
- `internal/trace`: diagnòstic dels nous fluxos;
- `internal/executor` i `internal/safety`: sense canvis de política.

Els tipus només es promouen a `internal/core` quan hi ha més d’un consumidor real, seguint la convenció actual.

### 13.2 Cutover

El protocol intern es reemplaça en un únic camí canònic:

- `explain` passa a `answer`;
- `prior_session_evidence` es divideix en `session_result` i `retry_observation`;
- s’afegeixen `operation`, `evidence_source`, `freshness`, `context_refs` i `context_revision`;
- s’afegeix `action=retrieve_context`;
- es conserva `current_observation`, `current_execution`, `evidence_revision` i `attempt_ids`;
- s’afegeix el refinament `reduce_output` sense modificar `repeat_reason`;
- no hi ha flag de compatibilitat ni dos workflows vius.

No s’afegeix una segona crida de routing. Les crides addicionals només existeixen quan el planner ja ha identificat context de sessió necessari o quan cal processar fragments.

## 14. Fluxos canònics

### 14.1 Reformatejar un resultat anterior

1. El prompt rep el catàleg de vuit resultats.
2. El model retorna `transform + session_result + snapshot + retrieve_context` amb `result-7`.
3. El runtime mostra l’estat breu i carrega `result-7`.
4. Si cal, processa tots els fragments.
5. El planner rep una revisió completa.
6. Retorna `complete` citant la revisió.
7. La UI mostra el Markdown; l’executor ha rebut zero invocacions.

### 14.2 Comparar dos resultats

1. El model selecciona `result-1` i `result-8`.
2. El runtime preserva la identitat de les dues fonts.
3. Tots els fragments produeixen notes amb procedència.
4. La reducció final compara només dades atribuïbles a cada resultat.
5. `complete` cita una revisió que cobreix ambdues referències.

### 14.3 Consultar estat actual després d’un resultat històric

1. L’usuari demana tornar a comprovar una dada mutable.
2. El model selecciona `observe + current_observation + current`.
3. El runtime no admet `session_result` com a prova actual.
4. El planner proposa una observació acotada al pressupost.
5. La finalització cita intents i revisió d’evidència del workflow actual.

### 14.4 Refinar una observació truncada

1. La primera consulta produeix una sortida marcada com a truncada.
2. El model referencia l’intent i retorna `refinement_reason=reduce_output`.
3. El workflow admet un únic refinament per aquell intent arrel.
4. Després del refinament, el model completa o bloqueja; no inicia una tercera consulta equivalent.

## 15. Criteris d’acceptació

### 15.1 Context de sessió

- Qualsevol dels vuit resultats vius es pot seleccionar per ID.
- Diversos resultats es poden seleccionar en un mateix torn.
- Els IDs no canvien quan s’expulsa l’entrada més antiga.
- `/new` elimina totes les referències sense reutilitzar-ne els IDs; el final del procés elimina també el comptador.
- Un ID expulsat no selecciona accidentalment una altra entrada.
- Un resultat bloquejat es pot transformar com a text sense convertir-se en evidència d’èxit.

### 15.2 Autoritat i frescor

- `retrieve_context`, `answer`, `transform` i `capability` produeixen zero invocacions de l’executor.
- `/plan` produeix zero invocacions per a totes les operacions.
- Un snapshot històric no completa una consulta d’estat actual.
- Una observació actual no s’exigeix per reformatejar o resumir un resultat històric.
- Un resultat o fragment maliciós no pot habilitar execució.
- `/retry` conserva l’elegibilitat estricta de les observacions parcials actuals.

### 15.3 Fragments

- Tots els fragments admesos es consoliden exactament una vegada i en ordre, encara que el transport hagi necessitat retries.
- La revisió no es completa si falta o es duplica un fragment.
- Un error de fragment o consolidació no produeix un fals `complete`.
- No es presenta mai una previsualització o truncament com a resultat complet.
- La UI mostra una sola línia d’estat quan hi ha múltiples crides al model.
- Els resultats petits i grans segueixen el mateix contracte causal.

### 15.4 Observacions compactes

- El primer prompt inclou el pressupost d’evidència.
- El prompt demana filtratge, agregació i deduplicació anticipats sense comandos específics.
- Una pipeline de lectura necessària per acotar evidència no queda prohibida per defecte.
- `reduce_output` només referencia una sortida truncada real.
- Només s’admet un refinament `reduce_output` per intent arrel.
- La reproducció dels ports acaba amb una consulta inicial compacta o, com a màxim, una consulta i un refinament.

### 15.5 Regressió canònica

Una prova interactiva reprodueix:

1. observar els ports;
2. completar amb una llista íntegra;
3. demanar format Markdown sense repetir la descoberta;
4. recuperar el resultat anterior;
5. retornar exactament la llista Markdown;
6. verificar zero comandos en el segon torn;
7. verificar que no apareix cap afirmació falsa de truncament.

Una segona prova selecciona un resultat no immediat, i una tercera comprova comparació multireferència amb fragments.

## 16. Verificació i stop gates

La implementació és acceptable quan:

- les transicions, matriu de validació, IDs, revisions i admissió de refinaments tenen proves RED/GREEN;
- les proves del prompt cobreixen les combinacions coherents i les reparacions;
- les proves de sessió cobreixen retenció, expulsió, `/new`, retry i múltiples referències;
- les proves de fragments cobreixen UTF-8, ordre, completesa, error i cancel·lació;
- les proves del bucle demostren les fronteres de zero execució;
- la reproducció canònica passa sense executar una segona descoberta;
- les proves afectades, la suite completa, `go vet`, build i race detector dels paquets afectats passen;
- una revisió independent no detecta regressions d’autoritat, prompt injection, frescor o fals èxit;
- no queda cap heurística local basada en frases o idiomes.

Stop gates:

- si `session_result` pot completar una dada actual sense observació actual, cal tornar al disseny;
- si `retrieve_context` pot arribar a l’executor o a la política de confirmació, la implementació no és admissible;
- si la completesa exigeix interpretar semànticament el contingut dins del runtime, cal limitar-se a procedència i participació de fragments;
- si processar fragments pot publicar una resposta parcial com a completa, cal tornar al disseny;
- si la solució necessita un segon router o un classificador local de llenguatge, cal tornar al disseny;
- si limitar refinaments impedeix una descoberta realment dependent de nova informació, cal distingir-la estructuralment d’un refinament de sortida, no relaxar el límit global.

## 17. Documentació

El README documentarà breument:

- la diferència entre consultar estat actual i reutilitzar un resultat de sessió;
- que els vuit resultats viuen només durant la sessió i `/new` els elimina;
- que el processament per fragments pot fer més d’una crida al model i mostra un estat breu;
- que les traces opt-in poden contenir el context històric enviat al model;
- que la seguretat i les confirmacions de terminal no canvien.

No s’afegeixen flags ni configuració pública nova en aquesta feature.
