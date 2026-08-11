# Espaiat de diagnòstics i confirmació del límit de planificació

**Data:** 2026-08-11

**Estat:** aprovat per implementar

**Nivell:** BOUNDED

## Objectiu

Millorar la llegibilitat del terminal i el comportament de la confirmació que apareix quan Shellia arriba al límit de rondes de planificació.

El resultat observable serà:

- el valor per defecte de `planning_max_rounds` al projecte passarà de `4` a `5`;
- la configuració personal de l'usuari tindrà `planning_max_rounds = 10`, sense canviar cap altre valor;
- els warnings tindran una línia en blanc al davant, igual que els errors;
- el cursor de la pregunta `Continue planning? [y/n]:` quedarà immediatament després dels dos punts, no a la línia següent.

## Disseny

### Configuració

`internal/config` continuarà sent l'únic propietari del valor per defecte. Es canviarà la constant existent a `5`; la precedència actual de fitxer i variables d'entorn no canviarà.

La configuració personal es modificarà fora del repositori, al fitxer que resolgui el mecanisme de configuració existent. Només s'afegirà o actualitzarà `planning_max_rounds = 10` dins de `[execution]`, preservant la resta del contingut.

### Espaiat de warnings i errors

`printWarningTo` afegirà una línia en blanc abans del warning. `printErrorTo` ja delega a `renderPanel`, que comença amb una línia en blanc, i per tant no necessita cap canvi de producció.

L'espaiat es mantindrà als renderitzadors centrals perquè tots els seus consumidors rebin el mateix comportament. No s'introduirà estat per detectar separadors previs ni es tocaran call sites individuals.

### Cursor de confirmació

`renderPlanningLimitPrompt` reutilitzarà la mateixa fila editable que ja usa `renderConfirmationPrompt`. El contingut, els colors, les opcions i el processament de tecles es conservaran; només canviarà si el renderitzat finalitza la fila abans de llegir l'entrada.

No es crearà cap component nou ni es duplicarà el format de confirmació.

## Proves i verificació

Les proves focalitzades demostraran que:

- la configuració per defecte retorna `PlanningMaxRounds == 5`;
- un warning renderitzat comença amb una línia en blanc;
- un error conserva la línia en blanc existent;
- el prompt del límit deixa la fila editable amb el cursor després de `: `.

S'aplicarà RED/GREEN sobre les proves afectades. Després es farà `gofmt`, s'executarà la suite completa amb `env GOCACHE=/tmp/go-build go test -count=1 ./...` i es construirà el CLI.

La configuració personal es verificarà rellegint el fitxer i comprovant el valor efectiu sense exposar ni modificar altres camps.

## Fora d'abast

- Canviar la precedència de configuració o convertir `5` en un màxim obligatori.
- Alterar altres prompts, textos, colors o geometria de la UI.
- Afegir dependències o un gestor d'espaiat amb estat.
- Modificar cap altre valor de la configuració personal.
