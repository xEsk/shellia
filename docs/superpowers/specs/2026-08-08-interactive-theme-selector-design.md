# Selector interactiu de tema visual

## Objectiu

Afegir el comandament interactiu `/theme` perquè l’usuari pugui consultar i seleccionar un dels quatre temes visuals de Shellia sense editar manualment la configuració. La selecció s’ha de desar a `[ui].style` del `config.toml` i s’ha d’aplicar a la sessió actual sense reiniciar l’aplicació.

## Experiència d’usuari

- `/theme` mostra `plain`, `guide`, `bands` i `cards` al submenú de comandaments, amb `*` davant del tema configurat actualment.
- `/theme <nom>` selecciona el tema indicat. Els noms no distingeixen majúscules de minúscules i es normalitzen als valors canònics.
- El submenú filtra les opcions mentre s’escriu i Tab completa la primera coincidència, igual que `/model`.
- Els quatre temes sempre són visibles i seleccionables, inclòs `cards`, independentment de l’estat intern de la seva implementació visual.
- Després d’un canvi correcte, Shellia mostra una confirmació amb el nom del tema actiu.
- Un nom desconegut produeix un avís i no modifica ni la configuració persistent ni la sessió.

## Arquitectura

El canvi estendrà els propietaris existents, sense crear un sistema nou de selecció:

- `internal/interactive` reconeixerà `/theme` i `/theme <nom>`, exposarà el nou tipus de comandament i extraurà el nom sol·licitat.
- `internal/ui/ui_commandmenu.go` reutilitzarà el menú de slash commands per mostrar, filtrar i completar els quatre valors de `config.VisualStyle`.
- `internal/config` serà l’únic propietari de la validació i la persistència del tema. Una actualització textual focalitzada modificarà només `style` dins de `[ui]`, preservant la resta del fitxer i els comentaris. Si `[ui]` o `style` no existeixen, s’afegiran sense reordenar les altres seccions.
- `internal/app` coordinarà la selecció. Primer validarà i persistirà el nou valor; només després actualitzarà `cfg.VisualStyle` i substituirà `deps.Renderer`.

El nou renderer es construirà amb `effectivePresentation(cfg, deps)` i conservarà la identitat resolta per `[ui].prompt_identity` (`user` o `you`). Això manté també el comportament existent de `--no-color`, `TERM=dumb`, sortides no TTY i terminals sense capacitat visual. La sessió, l’historial, el mode interactiu i la memòria no es reiniciaran.

## Flux de dades i errors

1. El parser identifica `/theme` com un comandament local; no es fa cap petició a l’LLM.
2. Sense argument, el menú mostra les quatre opcions i el tema configurat actual.
3. Amb argument, `config.NormalizeVisualStyle` valida el nom sense canviar l’estat.
4. La configuració escriu el valor canònic a `[ui].style` mantenint els permisos originals del fitxer.
5. Només si l’escriptura finalitza correctament, l’aplicació actualitza la configuració en memòria, recalcula la presentació efectiva i crea el renderer nou.
6. Si la validació o l’escriptura fallen, es mostra un avís i es conserva el renderer anterior.

## Proves

Les proves focalitzades cobriran:

- reconeixement de `/theme` i extracció de l’argument;
- presència del comandament al menú principal;
- submenú amb els quatre temes, marca del tema actiu, filtratge i autocompleció;
- substitució i inserció de `style` dins de `[ui]` sense alterar el cos del TOML;
- selecció vàlida persistent i canvi immediat del renderer;
- tema desconegut i error de persistència sense canvis parcials;
- bucle interactiu sense peticions a l’LLM;
- compatibilitat amb els quatre valors, inclòs `cards`.

Després de les proves focalitzades s’executaran `gofmt`, la suite completa amb `env GOCACHE=/tmp/go-build go test -count=1 ./...` i la compilació del binari.

## Fora d’abast

- Canviar el disseny o el comportament intern dels quatre renderers.
- Afegir temes nous, colors configurables o plugins dinàmics.
- Reiniciar o esborrar l’historial de la sessió després del canvi.
- Canviar el tema d’una execució en curs; `/theme` només s’avalua al prompt principal entre torns.
