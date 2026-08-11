# Disseny de context de sessió referenciable i contracte d’evidència

**Data:** 2026-08-11

**Estat:** implementat

**Nivell:** CRITICAL

**Àmbit:** follow-ups sobre resultats anteriors, procedència i frescor de l’evidència, autoritat d’execució i observacions compactes

## 1. Resum

Shellia distingirà explícitament:

- l’operació que resol la petició;
- la font d’informació necessària;
- la frescor exigida;
- l’autoritat d’execució, que continuarà sent propietat immutable del runtime.

Una petició com «reformata la resposta anterior» recuperarà un resultat identificat de la sessió i executarà zero comandos. Una petició com «comprova els ports ara» continuarà requerint una observació actual. El runtime validarà combinacions estructurals; no buscarà frases, verbs ni idiomes concrets dins del missatge.

Els vuit resultats que la sessió interactiva ja manté rebran IDs estables. El planner podrà demanar-ne qualsevol amb una nova decisió `retrieve_context`, separada d’`execute`. El contingut es recuperarà complet dins d’un pressupost dedicat; si no hi cap, Shellia es bloquejarà explícitament en lloc de truncar-lo o repetir una descoberta.

El prompt també coneixerà el pressupost d’evidència abans de la primera ordre i demanarà sortides filtrades, agregades i deduplicades quan una observació pugui ser extensa. Aquesta millora és general i no conté coneixement específic de `lsof`, ports o plataformes.

## 2. Problema

El contracte actual protegeix correctament les consultes d’estat actual contra evidència obsoleta, però no representa una resposta que només transforma text d’un torn anterior.

En la reproducció canònica:

1. Shellia observa els ports i presenta la llista completa;
2. l’usuari demana la mateixa resposta en format Markdown i prohibeix repetir la descoberta;
3. el model produeix correctament la llista Markdown sense comandos;
4. el runtime rebutja `complete` perquè la base pertany al workflow anterior;
5. la reparació d’`observe` obliga el model a refrescar l’estat;
6. Shellia executa de nou i acaba treballant amb projeccions truncades.

El resultat complet encara existia a l’historial quan Shellia va rebutjar la primera resposta correcta. Per tant, el primer límit trencat és el contracte de procedència i frescor, no la captura del comando.

La mateixa sessió va necessitar quatre batches per compactar progressivament la sortida inicial. El primer prompt no orientava prou el model a adaptar la consulta al pressupost d’evidència abans d’executar.

## 3. Resultat observable

| Entrada | Operació | Font | Frescor | Comportament |
| --- | --- | --- | --- | --- |
| `Quins ports estan oberts ara?` | `observe` | `current_observation` | `current` | Executar la consulta mínima i respondre amb evidència actual. |
| `Reformata la resposta anterior` | `answer` | `session_result` | `snapshot` | Recuperar el resultat citat i executar zero comandos. |
| `Resumeix el primer resultat` | `answer` | `session_result` | `snapshot` | Recuperar aquell resultat per ID i resumir-lo. |
| `Compara el primer resultat amb l’últim` | `answer` | `session_result` | `snapshot` | Recuperar els dos resultats si caben al pressupost conjunt. |
| `Torna a comprovar els ports` | `observe` | `current_observation` | `current` | Fer una observació nova. |
| `Pots comprovar els ports?` | `capability` | `model_knowledge` | `not_applicable` | Explicar la capacitat i oferir-la sense executar. |

`answer` inclou explicar, resumir, comparar, traduir i reformatejar. Són categories semàntiques del protocol, no paraules clau locals.

## 4. Objectius i fora d’abast

### 4.1 Objectius

- Referenciar qualsevol dels vuit resultats vius de la sessió.
- Recuperar completament el contingut que cap al pressupost dedicat.
- Separar operació, font, frescor i autoritat.
- Impedir que recuperar context arribi a l’executor.
- Preservar `/retry`, `/plan`, la seguretat i les confirmacions actuals.
- Evitar truncament silenciós i redescobertes causades per un follow-up textual.
- Orientar la primera observació cap a una sortida compacta.

### 4.2 Fora d’abast

- Processar resultats massa grans per fragments o map/reduce.
- Persistir historial després de tancar Shellia.
- Compartir historial entre processos o conservar-lo després de `/new`.
- Afegir una base de dades, fitxers d’historial o un servei nou.
- Afegir un router LLM separat o una crida de classificació prèvia.
- Detectar intencions amb regex, paraules clau o llistes dependents de l’idioma.
- Afegir equivalència semàntica general entre comandos.
- Afegir una nova política runtime de refinaments de sortida.
- Canviar la classificació de risc, `yes_safe`, els timeouts o les confirmacions.

El processament per fragments i l’admissió estructurada de refinaments queden com a possibles extensions futures només si apareixen casos reals que les necessitin.

## 5. Contracte de decisió

```text
action:
  execute | retrieve_context | complete | blocked

operation:
  answer | observe | act | capability

evidence_source:
  model_knowledge | session_result | retry_observation |
  current_observation | current_execution

freshness:
  not_applicable | snapshot | current

success_criteria:
  resultat concret que tanca l’objectiu

context_refs:
  IDs de resultats de sessió

completion_basis:
  source
  freshness
  context_revision opcional
  evidence_revision opcional
  attempt_ids opcionals
```

### 5.1 Autoritat immutable

El runtime continua derivant `executionAllowed` del mode real d’entrada. El model no retorna ni modifica aquest valor.

- `/plan` manté `executionAllowed=false`.
- `retrieve_context` mai invoca l’executor ni demana confirmacions de terminal.
- `answer` i `capability` no admeten `action=execute`.
- `observe` i `act` només poden arribar a l’executor quan l’autoritat immutable ho permet.
- Cap resultat històric, oferta o text recuperat pot habilitar execució.

### 5.2 Bloqueig del contracte

El workflow només bloqueja `operation`, `evidence_source`, `freshness` i `success_criteria` després d’una decisió coherent.

Exemples de reparació abans del bloqueig:

- `answer + current_observation + execute` es repara cap a `session_result + snapshot + retrieve_context` quan la petició depèn de l’historial;
- `observe + session_result + current` es repara cap a una observació actual;
- `complete + session_result` sense context carregat es repara cap a `retrieve_context`.

La reparació mai pot convertir una operació no executable en autoritat de terminal.

## 6. Matriu de validació

| Operació | Font | Frescor | Accions admissibles |
| --- | --- | --- | --- |
| `answer` | `model_knowledge` | `not_applicable` | `complete`, `blocked` |
| `answer` | `session_result` | `snapshot` | `retrieve_context`, `complete`, `blocked` |
| `observe` | `current_observation` | `current` | `execute`, `complete`, `blocked` |
| `observe` | `retry_observation` | `current` | `execute`, `complete`, `blocked`, només per al retry elegible |
| `act` | `current_execution` | `current` | `execute`, `complete`, `blocked` |
| `act` | `current_observation` | `current` | `execute`, `complete`, `blocked` quan demostra la postcondició |
| `capability` | `model_knowledge` | `not_applicable` | `complete`, `blocked` |

Regles:

- `complete + session_result` requereix una `context_revision` carregada que cobreixi tots els `context_refs`;
- `complete + current_observation/current_execution` conserva les validacions actuals d’`evidence_revision` i `attempt_ids`;
- `retry_observation` conserva el lligam estricte amb `LastRetryInstruction` i `LastObservationObjective`;
- un resultat bloquejat o fallit es pot citar com a text, però no prova que `observe` o `act` s’hagin completat;
- un snapshot històric no és compatible amb `freshness=current`;
- `complete` no pot contenir comandos ni recuperacions pendents.

## 7. Resultats de sessió

L’entrada d’historial existent s’amplia:

```text
SessionResult
  id
  instruction
  outcome
  result
  character_count
```

### 7.1 Identitat i retenció

- Es reutilitza el màxim actual de vuit entrades.
- Els IDs són monotònics durant la vida del procés: `result-1`, `result-2`, etc.
- Expulsar l’entrada més antiga no renumera ni reutilitza IDs.
- `/new` buida el catàleg però no reinicia el comptador dins del mateix procés.
- Tancar Shellia elimina el catàleg i el comptador.
- No s’afegeix persistència ni configuració paral·lela de retenció.

Qualsevol outcome conservat és referenciable com a text. La validesa com a evidència causal depèn de la matriu anterior.

### 7.2 Catàleg del prompt

La primera ronda rep només:

```text
id
instruction
outcome
character_count
preview
```

La previsualització serveix per seleccionar una referència, però no permet completar una resposta basada en aquell contingut. El runtime exigeix `retrieve_context`.

## 8. Recuperació acotada

### 8.1 Flux

1. El planner rep l’objectiu i el catàleg.
2. Retorna `action=retrieve_context` amb `context_refs` no buits.
3. `internal/app` valida que tots els IDs existeixen.
4. Calcula la mida conjunta del contingut seleccionat.
5. Si cap al pressupost dedicat, crea una `context_revision` completa i torna a planificar amb el contingut íntegre.
6. `complete` només s’admet si cita aquella revisió i totes les referències carregades.

La UI mostra una única línia perquè la recuperació implica una crida addicional al model:

```text
Recuperant 2 resultats de la sessió…
```

`retrieve_context` no crea plans, intents de comando ni confirmacions.

### 8.2 Pressupost

El contingut recuperat té un pressupost intern conjunt de 16.000 caràcters per torn. És un límit de capacitat, no una heurística lingüística.

- Si una referència o el conjunt seleccionat supera el límit, el runtime acaba en `blocked/unavailable`.
- El missatge identifica els resultats afectats i la mida requerida.
- Shellia no injecta una versió parcial, no afirma que l’historial s’hagi perdut i no substitueix la recuperació per una nova descoberta.
- No s’afegeix configuració pública fins que hi hagi una necessitat real de producte.

## 9. Prompt per a observacions compactes

La primera ronda rep sempre el pressupost d’evidència de comandos, encara que encara no hi hagi observacions.

El prompt estable afegeix aquestes regles:

- demanar només els camps necessaris per als criteris d’èxit;
- filtrar, agregar i deduplicar a l’origen quan una sortida pugui superar el pressupost;
- evitar una consulta àmplia seguida de consultes que només reformategen la mateixa informació;
- permetre pipelines de lectura quan siguin necessàries per acotar l’evidència;
- completar sense executar quan l’evidència actual ja conté el valor exacte;
- si una sortida queda truncada, preferir una única consulta compacta que substitueixi la forma de sortida anterior.

Aquesta feature no afegeix camps de refinament ni una comprovació local d’equivalència semàntica. La millora es limita al contracte del prompt i a proves que en fixen el contingut.

## 10. Errors i recuperació

- ID desconegut o expulsat: `blocked/missing_input`, zero executor.
- Catàleg buit: el model no pot inventar una referència.
- Contingut superior a 16.000 caràcters: `blocked/unavailable`, sense truncament ni redescoberta.
- Error del proveïdor durant la ronda posterior: reutilitza retries, timeout i cancel·lació actuals.
- Cancel·lació: no publica cap `context_revision` parcial i conserva l’objectiu per a `/retry` quan correspon.
- `retrieve_context` amb referències inexistents no pot derivar en una descoberta substitutiva.
- Una contradicció persistent acaba sense fals èxit.

## 11. Confiança, privacitat i traces

Els resultats recuperats són dades no fiables:

- es delimiten fora de les instruccions del sistema;
- no poden canviar l’objectiu, la frescor o l’autoritat;
- no poden injectar comandos ni marcar intents com a executats;
- les instruccions contingudes dins d’un resultat anterior no obtenen autoritat nova.

No es persisteix contingut per defecte. Amb traces opt-in, el resultat recuperat apareix dins del prompt complet igual que qualsevol altre context enviat al model; això es documentarà.

S’afegeixen només dos esdeveniments:

- `context_retrieval_requested`;
- `context_revision`.

## 12. Propietaris i migració

### 12.1 Propietaris

- `internal/app`: workflow, autoritat, IDs, pressupost i revisions de context;
- `internal/llm`: contracte JSON, catàleg, prompt i validació estructural;
- `internal/session`: projecció de memòria curta i elegibilitat de retry;
- `internal/ui`: estat breu de recuperació;
- `internal/trace`: diagnòstic dels dos nous esdeveniments;
- `internal/executor` i `internal/safety`: sense canvis de política.

### 12.2 Cutover

- `explain` passa a `answer`;
- `prior_session_evidence` es divideix en `session_result` i `retry_observation`;
- s’afegeixen `evidence_source`, `freshness`, `context_refs` i `context_revision`;
- s’afegeix `action=retrieve_context`;
- es conserven `current_observation`, `current_execution`, `evidence_revision` i `attempt_ids`;
- no hi ha flag de compatibilitat ni dos workflows vius.

No s’afegeix cap router. Les crides addicionals només apareixen després que el planner demani un resultat de sessió concret.

## 13. Criteris d’acceptació

### 13.1 Historial i autoritat

- Qualsevol dels vuit resultats vius es pot seleccionar per ID.
- Diversos resultats es poden seleccionar si la mida conjunta no supera 16.000 caràcters.
- Els IDs no canvien amb l’expulsió i `/new` no els reutilitza dins del procés.
- `retrieve_context`, `answer` i `capability` executen zero comandos.
- `/plan` executa zero comandos per a totes les operacions.
- Un resultat maliciós no pot activar l’executor.
- Un snapshot històric no completa una consulta d’estat actual.
- `/retry` conserva les observacions parcials elegibles.

### 13.2 Recuperació

- El contingut dins del pressupost arriba complet a la ronda posterior.
- `complete` referencia una revisió real i totes les fonts requerides.
- Un ID expulsat no selecciona una altra entrada.
- El contingut massa gran produeix un bloqueig explícit amb mida i referència.
- Mai es presenta una previsualització o truncament com a resultat complet.
- Mai es repeteix una descoberta per substituir una recuperació històrica fallida.
- La UI mostra una sola línia d’estat per recuperació.

### 13.3 Observacions compactes

- El primer prompt inclou el pressupost d’evidència.
- El prompt demana filtratge, agregació i deduplicació anticipats.
- Una pipeline de lectura necessària per acotar evidència no queda prohibida per defecte.
- La reproducció dels ports es resol amb una consulta inicial compacta o una única correcció de format després d’un truncament.

### 13.4 Regressió canònica

Una prova interactiva reprodueix:

1. observar els ports;
2. completar amb una llista íntegra;
3. demanar format Markdown sense repetir la descoberta;
4. recuperar el resultat anterior;
5. retornar la llista Markdown;
6. verificar zero comandos en el segon torn;
7. verificar que no apareix cap afirmació falsa de truncament.

Una segona prova selecciona un resultat no immediat i una tercera comprova una comparació multireferència dins del pressupost.

## 14. Verificació i stop gates

La implementació és acceptable quan:

- contracte, matriu, IDs, pressupost i revisions tenen proves RED/GREEN;
- els tests del prompt cobreixen combinacions coherents i reparacions;
- els tests de sessió cobreixen retenció, expulsió, `/new`, retry i múltiples referències;
- els tests del bucle demostren les fronteres de zero execució;
- la reproducció canònica passa sense una segona descoberta;
- les proves afectades, la suite completa, `go vet`, build i race detector dels paquets afectats passen;
- una revisió independent no detecta regressions d’autoritat, frescor, prompt injection o fals èxit;
- no queda cap heurística local basada en frases o idiomes.

Stop gates:

- si `session_result` pot completar una dada actual, cal tornar al disseny;
- si `retrieve_context` pot arribar a l’executor, la implementació no és admissible;
- si el runtime necessita interpretar semànticament el contingut històric, cal limitar-se a procedència i referències;
- si el límit de 16.000 caràcters causa una necessitat real recurrent, cal dissenyar fragmentació en una feature separada;
- si la solució necessita un segon router o un classificador local de llenguatge, cal tornar al disseny.

## 15. Documentació

El README documentarà breument:

- la diferència entre estat actual i resultat de sessió;
- que els vuit resultats viuen només durant el procés i `/new` els elimina;
- que una recuperació pot fer una crida addicional al model;
- que resultats superiors al pressupost es bloquegen sense truncament;
- que les traces opt-in poden contenir el context recuperat;
- que la seguretat i les confirmacions de terminal no canvien.

No s’afegeixen flags ni configuració pública nova.
