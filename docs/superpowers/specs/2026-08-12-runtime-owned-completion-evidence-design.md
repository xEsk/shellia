# Disseny de finalització amb evidència propietat del runtime

**Data:** 2026-08-12

**Estat:** implementat i verificat

**Nivell:** CRITICAL

**Àmbit:** contracte del planner, finalització causal, evidència del workflow i errors visibles del model

## 1. Resultat

Shellia conservarà la qualitat actual dels plans, la distinció entre `answer`, `observe`, `act` i `capability`, i totes les fronteres d’execució. El model, però, deixarà de copiar metadades internes de Shellia per poder acabar un torn.

El model decidirà què cal fer i què cal respondre. Shellia continuarà sent l’únic propietari de revisions, intents, procedència, frescor, autoritat i confirmacions. Quan el model retorni `complete`, el runtime associarà automàticament l’evidència vàlida que ja controla.

El resultat observable és que una resposta útil no fallarà perquè el model hagi barrejat `attempt_ids`, hagi repetit una revisió antiga o hagi fet discordar dues còpies de la mateixa font. Els errors causats només per aquesta comptabilitat interna no arribaran a l’usuari com «invalid model response».

## 2. Usuari, problema i valor actual

L’usuari és qui fa servir Shellia com a agent de terminal interactiu o one-shot.

El planner actual ha de resoldre l’objectiu i, alhora, reproduir aquest protocol intern:

- `evidence_source`;
- `freshness`;
- `completion_basis.source`;
- `completion_basis.freshness`;
- `context_revision`;
- `evidence_revision`;
- `attempt_ids`.

Shellia ja coneix tots aquests valors. Demanar-los també al model crea contradiccions sense aportar autoritat nova.

La reproducció del 2026-08-11 ho mostra: després d’obtenir correctament els ports, el model va citar intents `[5,6,7,8]` amb la revisió `6`; Shellia va rebutjar els intents `5` i `6`, que pertanyien a la revisió `5`. En reparar-ho, el model va canviar una còpia de la font a `current_execution` i el contracte immutable ho va tornar a rebutjar. La resposta final era útil i l’evidència existia; va fallar només la comptabilitat interna.

El valor d’aquesta feature és recuperar la simplicitat del flux original sense perdre els millors plans ni les proteccions introduïdes després.

## 3. Abast mínim

### 3.1 Dins d’abast

- Eliminar de la resposta del model `evidence_source`, `freshness` i `completion_basis`.
- Fer que `internal/app` resolgui i validi internament la procedència de `complete`.
- Conservar `operation` i `success_criteria` com a contracte semàntic bloquejat.
- Conservar `context_refs` només per seleccionar resultats amb `retrieve_context`; el model no els haurà de repetir en `complete`.
- Simplificar el prompt, l’esquema JSON i les reparacions semàntiques d’acord amb el contracte nou.
- Registrar a traces la procedència resolta pel runtime.
- Cobrir la reproducció dels ports i les fronteres d’autoritat amb proves.

### 3.2 Fora d’abast

- Canviar la qualitat o l’estratègia dels comandos proposats.
- Reduir el nombre de rondes que el model usa per compactar una observació.
- Canviar la classificació local de risc, `yes_safe`, confirmacions o `/plan`.
- Eliminar `operation`, `success_criteria`, `purpose`, `repeat_reason`, interactivitat o dependències entre comandos.
- Afegir un router, un segon model, heurístiques lingüístiques o interpretació semàntica local de stdout/stderr.
- Canviar la retenció, mida o persistència dels resultats de sessió.
- Ocultar errors persistents del proveïdor o executar davant d’una resposta que no es pot validar estructuralment.

## 4. Propietaris existents

La feature reutilitza els propietaris actuals:

- `internal/llm` conserva el contracte de decisió, el prompt, Structured Outputs i el parsing;
- `internal/app/workflow.go` continua sent l’únic propietari de l’autoritat, els intents, les revisions i l’admissió de `complete`;
- `internal/app/turn.go` continua dirigint reparació, execució i finalització;
- `internal/session` conserva resultats i retry sense canviar persistència;
- `internal/executor` i `internal/safety` no canvien de política;
- `internal/trace` registra el diagnòstic derivat pel runtime.

No es crea cap servei, esquema persistent, flag ni abstracció paral·lela.

## 5. Contracte simplificat del model

La resposta de planificació serà:

```json
{
  "action": "execute|retrieve_context|complete|blocked",
  "operation": "answer|observe|act|capability",
  "success_criteria": "resultat concret",
  "summary": "pla o resposta final",
  "context_refs": [],
  "offer": {"objective": "", "summary": ""},
  "blocker_kind": "",
  "blocker_reason": "",
  "commands": []
}
```

L’estructura de cada comando es conserva sense canvis. En particular, la senyalització de risc del model només pot elevar la política local, mai reduir-la.

Regles:

- `context_refs` només és no buit amb `action=retrieve_context`;
- després de carregar context, `complete` no repeteix IDs ni revisions;
- `complete` no conté comandos;
- `blocked` continua requerint `blocker_kind` i `blocker_reason`;
- `capability` conserva `offer` i executa zero comandos;
- Structured Outputs continua sent el camí preferit quan el perfil el suporta.

## 6. Contracte immutable

La primera decisió coherent bloqueja només:

- `operation`;
- `success_criteria`.

En rondes posteriors, Shellia projecta aquests valors al prompt i normalitza `success_criteria` al valor inicial abans de validar. Un canvi d’`operation` continua sent una contradicció semàntica, especialment si travessa la frontera executable (`act`/`observe`) i no executable (`answer`/`capability`).

L’autoritat real `executionAllowed` mai forma part de la resposta del model i continua immutable des de la creació del workflow.

## 7. Resolució de `complete` pel runtime

El runtime resol una base causal interna; el model no la declara.

| Operació | Condició d’admissió | Procedència interna |
| --- | --- | --- |
| `answer` sense context carregat | resposta final estructuralment vàlida | `model_knowledge/not_applicable` |
| `answer` després de `retrieve_context` | existeix una revisió completa carregada | `session_result/snapshot`, amb refs i revisió internes |
| `capability` | resposta final sense comandos | `model_knowledge/not_applicable` |
| `observe` | existeix almenys un intent observat del workflow actual | `current_observation/current`, amb snapshot intern dels intents observats |
| `observe` sense intents actuals | hi ha una observació de retry exactament elegible | `retry_observation/current` |
| `act` | l’últim batch amb evidència conté almenys una execució reeixida | `current_execution/current`, amb els intents reeixits del batch associats internament |

Per a `observe`, són observables els intents executats encara que acabin amb error o timeout, perquè stdout/stderr també pot explicar l’estat. No compten comandos omesos, rebutjats, declinats o cancel·lats.

Per a `act`, un èxit antic no permet completar després d’un batch posterior sense cap execució reeixida. Això evita que una descoberta reeixida tapi una mutació posterior fallida. El runtime valida causalitat i ordre; el model continua sent responsable de decidir si el contingut satisfà realment `success_criteria`.

Per a `observe`, l’snapshot causal és acumulatiu dins del workflow actual. Això permet sintetitzar observacions de diverses rondes sense obligar el model a triar una sola revisió de batch. Per a `act`, l’admissió continua lligada al darrer batch, encara que la traça conservi el workflow complet. Cap dels dos incorpora resultats d’altres workflows ni converteix historial en estat actual.

## 8. Flux mínim

### 8.1 Observació o acció

1. El model fixa `operation` i `success_criteria`.
2. Retorna `execute` amb el pla mínim.
3. Shellia classifica, mostra, confirma i executa com ara.
4. Shellia registra intents i evidència sense exposar-ne els IDs com a camps de sortida obligatoris.
5. El model rep les observacions i retorna `execute`, `blocked` o `complete`.
6. Si retorna `complete`, Shellia resol la procedència interna i només l’admet si existeix evidència compatible.

### 8.2 Resultat de sessió

1. El model selecciona IDs reals amb `retrieve_context`.
2. Shellia carrega el conjunt complet i en conserva refs i revisió.
3. El model respon amb `complete` sense repetir `context_refs` ni `context_revision`.
4. Shellia associa automàticament el context carregat.

### 8.3 Sense evidència suficient

Si `observe` o `act` retorna `complete` abans de tenir evidència admissible, Shellia demana una reparació semàntica concreta: executar, observar o bloquejar-se. No inventa evidència i no presenta fals èxit.

## 9. Autoritat i confiança

No canvien aquestes fronteres:

- només `act` i `observe` poden arribar a l’executor;
- `/plan` conserva `executionAllowed=false`;
- `answer`, `capability` i `retrieve_context` executen zero comandos;
- una oferta acceptada inicia un workflow nou i passa per la política normal;
- output, historial i context recuperat continuen sent dades no fiables;
- cap text del model redueix risc ni evita una confirmació local;
- associar evidència automàticament no autoritza una nova execució.

El canvi elimina autoritat aparent del model: els IDs i revisions deixen de ser declaracions seves i passen a ser dades exclusives del runtime.

## 10. Errors i recuperació

- JSON mal format o incompatible amb l’esquema: conserva el pressupost estructural finit actual; mai executa una decisió no parsejada.
- `complete` d’`observe` sense observació actual ni retry elegible: una reparació semàntica; després, error final sense fals èxit.
- `complete` d’`act` sense èxit al darrer batch: una reparació semàntica; després, error final sense fals èxit.
- canvi d’`operation`: reparació semàntica existent, sense canviar l’autoritat.
- `retrieve_context` amb IDs inexistents o massa grans: bloqueig actual, sense redescoberta substitutiva.
- rebuig estructurat del proveïdor: continua sent `blocked/unsafe_to_continue`.

Els detalls tècnics es mantenen a la traça. El missatge visible ha de descriure que Shellia no ha pogut completar el torn i proposar retry, però no culpar l’usuari ni mostrar comptabilitat d’intents o revisions.

## 11. Traces i observabilitat

`completion_validation` es conserva, però els camps de procedència provenen del runtime:

- operació i criteri bloquejats;
- font i frescor resoltes;
- context revision i refs internes, si correspon;
- evidence revision i intents interns, si correspon;
- `admitted` i motiu de rebuig.

La resposta crua del model ja no pot contradir aquestes dades perquè no les conté. No canvia la política opt-in ni el contingut sensible capturat.

## 12. Criteris d’acceptació

### 12.1 Robustesa visible

- La reproducció dels ports pot completar després de múltiples batches sense retornar revisions ni IDs.
- Barrejar informació observada en rondes diferents del mateix workflow no provoca un error de protocol.
- Una finalització correcta no entra en reparació per discrepàncies de font o frescor duplicades.
- Els camps eliminats no apareixen al prompt, l’esquema JSON ni la resposta parsejada.

### 12.2 Finalització causal

- `observe + complete` sense evidència actual ni retry elegible és rebutjat.
- `observe + complete` després d’un intent observat és admès i la traça associa l’evidència interna.
- `act + complete` sense cap execució reeixida al darrer batch és rebutjat.
- Una descoberta reeixida seguida d’una mutació fallida no pot acabar en èxit reutilitzant la descoberta.
- `act + complete` després d’una execució actual reeixida és admès.
- Cap evidència d’un workflow anterior s’associa automàticament a `act` o `observe`.

### 12.3 Context i sessió

- `answer` basat en coneixement pot completar sense context.
- Després de `retrieve_context`, `complete` usa exactament la revisió i els resultats carregats sense exigir que el model els repeteixi.
- Una preview del catàleg no es converteix en context carregat.
- Retry només usa l’observació anterior quan el lligam exacte actual continua sent elegible.

### 12.4 Autoritat i regressió

- `answer`, `capability`, `retrieve_context` i `/plan` produeixen zero execucions fora dels camins ja autoritzats.
- Un canvi d’operació executable a no executable, o a l’inrevés, no crea ni elimina autoritat.
- Classificació local, risc elevat pel model, confirmacions, repeticions i comandos interactius conserven el comportament actual.
- Structured Outputs, fallback `json_object` i perfils sense `response_format` continuen funcionant.

## 13. Verificació i evidència durable

La implementació seguirà RED/GREEN sobre proves del contracte i del bucle:

1. actualitzar fixtures perquè el contracte mínim sigui executable sense metadades d’evidència;
2. afegir proves focals de resolució runtime per cada fila de la taula;
3. afegir la regressió exacta de múltiples batches dels ports;
4. provar que les traces mostren procedència derivada, no copiada;
5. executar les suites afectades, la suite completa, `go vet`, build i detector de races dels paquets afectats;
6. obtenir una revisió independent centrada en autoritat, fals èxit, context no fiable i compatibilitat de proveïdor.

No es considera complet només perquè desapareguin errors de parsing: han de passar també les proves negatives d’autoritat i causalitat.

## 14. Riscos i mitigacions

### 14.1 Fals `complete` després d’una descoberta

Mitigació: `act` només pot usar el darrer batch amb evidència i aquest ha de contenir almenys un èxit. Una fallada posterior invalida l’ús d’un èxit antic com a base estructural.

### 14.2 Evidència massa àmplia

Mitigació: l’snapshot és exclusiu del workflow actual, les projeccions del prompt continuen acotades i la traça conserva els intents exactes. El runtime no interpreta stdout/stderr.

### 14.3 Pèrdua de diagnòstic

Mitigació: la informació eliminada del JSON del model no s’elimina de l’estat ni de les traces; simplement es deriva d’una font fiable.

### 14.4 Regressió de compatibilitat

Mitigació: no hi ha compatibilitat dual ni flag. El prompt, l’esquema, el parser, els fixtures i el workflow canvien junts en un únic cutover intern.

## 15. Stop gates

La implementació s’atura i torna al disseny si:

- necessita interpretar semànticament output arbitrari per decidir `complete`;
- pot associar historial o una preview a estat actual sense `retrieve_context` o retry elegible;
- permet completar `act` sense cap èxit actual compatible;
- obliga a relaxar `/plan`, confirmacions o classificació local;
- reintrodueix IDs, revisions o fonts duplicades al contracte del model sota un altre nom;
- necessita un segon router o una nova capa persistent.

## 16. Documentació i stop condition

Aquesta especificació substitueix les parts del disseny d’intenció i del contracte d’evidència que feien el model responsable de `evidence_source`, `freshness` i `completion_basis`. La resta d’invariants continua vigent.

No cal configuració ni documentació d’usuari nova. El README només s’actualitzarà si actualment exposa aquests camps interns; la documentació de `supports_json_schema` es conserva.

La feature s’atura quan els criteris anteriors passen, la reproducció dels ports acaba amb resposta útil i una revisió independent confirma que la simplificació no ha ampliat autoritat ni ha introduït falsos èxits.

No requereix una fase ni un pla de projecte separats: és una única feature delimitada i es pot lliurar directament a `implement-project-feature`.
