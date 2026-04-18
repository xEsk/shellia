package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	colorReset   = "\033[0m"
	colorBold    = "\033[1m"
	colorDim     = "\033[2m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorWhite   = "\033[97m"
)

type prefixedWriter struct {
	box     *stepBox
	started bool
	buffer  string
}

type directShellWriter struct {
	ui        bool
	target    io.Writer
	lineStart bool
	started   bool
}

// Write aplica un prefix visual a cada línia de sortida del shell.
// Retorna len(data) en cas d'èxit, o 0 + error si l'escriptura al target falla.
func (writer *prefixedWriter) Write(data []byte) (int, error) {
	writer.buffer += string(data)
	for len(writer.buffer) > 0 {
		if !writer.started {
			if writer.box != nil {
				writer.box.OutputLabel()
			}
			writer.started = true
		}

		newlineIndex := strings.IndexByte(writer.buffer, '\n')
		if newlineIndex == -1 {
			return len(data), nil
		}

		chunk := strings.TrimSuffix(writer.buffer[:newlineIndex+1], "\n")
		if writer.box != nil {
			writer.box.OutputLine(chunk)
		}
		writer.buffer = writer.buffer[newlineIndex+1:]
	}

	return len(data), nil
}

// Flush força la impressió de l'última línia parcial del buffer si n'hi ha.
func (writer *prefixedWriter) Flush() error {
	if !writer.started || writer.buffer == "" {
		return nil
	}
	if writer.box != nil {
		writer.box.OutputLine(writer.buffer)
	}
	writer.buffer = ""
	return nil
}

// Write imprimeix directament la sortida del mode :shell amb un to més discret.
// Manté les línies alineades a la columna zero i pinta cada línia en gris.
func (writer *directShellWriter) Write(data []byte) (int, error) {
	text := string(data)
	for len(text) > 0 {
		if writer.lineStart {
			if !writer.started {
				if _, err := fmt.Fprint(writer.target, "\n"); err != nil {
					return 0, err
				}
				writer.started = true
			}
			if _, err := fmt.Fprint(writer.target, styleStart(writer.ui, colorDim)); err != nil {
				return 0, err
			}
			writer.lineStart = false
		}

		newlineIndex := strings.IndexByte(text, '\n')
		if newlineIndex == -1 {
			if _, err := fmt.Fprint(writer.target, text); err != nil {
				return 0, err
			}
			return len(data), nil
		}

		chunk := text[:newlineIndex]
		if _, err := fmt.Fprint(writer.target, chunk, styleEnd(writer.ui), "\n"); err != nil {
			return 0, err
		}
		writer.lineStart = true
		text = text[newlineIndex+1:]
	}

	return len(data), nil
}

// Flush tanca l'última línia parcial del mode :shell si n'hi ha.
func (writer *directShellWriter) Flush() error {
	if writer.lineStart {
		return nil
	}
	_, err := fmt.Fprint(writer.target, styleEnd(writer.ui))
	return err
}

// uiEnabled indica si la sortida enriquida pot usar color ANSI.
func uiEnabled(cfg config) bool {
	if cfg.NoColor {
		return false
	}

	termName := strings.TrimSpace(os.Getenv("TERM"))
	if termName == "" || termName == "dumb" {
		return false
	}

	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}

	return (info.Mode() & os.ModeCharDevice) != 0
}

// exitWithError imprimeix un error i finalitza el procés amb el codi indicat.
func exitWithError(ui bool, message string, code int) {
	renderPanel(os.Stderr, ui, "error", colorRed, []string{
		style(ui, colorWhite+colorBold, message),
	})
	os.Exit(code)
}

// printContext mostra el context detectat quan s'activa el mode debug.
func printContext(ui bool, ctxInfo contextInfo) {
	lines := []string{
		metaLine(ui, "cwd", ctxInfo.CWD),
		metaLine(ui, "user", ctxInfo.User),
		metaLine(ui, "os", ctxInfo.OS),
		metaLine(ui, "shell", ctxInfo.Shell),
		metaLine(ui, "git", strconv.FormatBool(ctxInfo.Git.IsRepo)),
		metaLine(ui, "branch", fallbackValue(ctxInfo.Git.Branch, "-")),
	}
	if ctxInfo.Git.StatusShort == "" {
		lines = append(lines, metaLine(ui, "status", style(ui, colorDim, "(clean or empty)")))
	} else {
		lines = append(lines, fmt.Sprintf("%s\n%s", metaLabel(ui, "status"), indentLines(ctxInfo.Git.StatusShort, shellStreamPrefix(ui))))
	}
	renderPanel(os.Stdout, ui, "context", colorBlue, lines)
}

// printPlan presenta el resum i els comandos proposats pel model.
func printPlan(ui bool, cfg config, summary string, plans []commandPlan, discovery bool) {
	title := "plan"
	titleColor := colorMagenta
	if discovery {
		title = "discovery"
		titleColor = colorCyan
	}
	renderPanel(os.Stdout, ui, title, titleColor, []string{style(ui, colorWhite+colorBold, summary)})

	if len(plans) == 0 || !cfg.Verbose {
		return
	}

	lines := make([]string, 0, len(plans)+1)
	for index, plan := range plans {
		lines = append(lines,
			fmt.Sprintf("%s %s", stepBadge(ui, index+1), plan.Purpose),
			fmt.Sprintf("%s %s", metaLabel(ui, "command"), style(ui, colorWhite, plan.Command)),
			fmt.Sprintf("%s %s", metaLabel(ui, "risk"), riskBadge(ui, plan.Risk)),
			fmt.Sprintf("%s %s", metaLabel(ui, "safety"), classificationBadge(ui, plan.Classification)),
			fmt.Sprintf("%s %s", metaLabel(ui, "confirm"), confirmBadge(ui, plan.RequiresConfirmation)),
			"",
		)
	}
	renderPanel(os.Stdout, ui, "steps", colorDim, trimTrailingBlankLines(lines))
}

// printHeader mostra una capçalera compacta amb l'estat global de la sessió.
func printHeader(ui bool, ctxInfo contextInfo) {
	fmt.Println()
	fmt.Println(shelliaBrand(ui, false) + style(ui, colorDim, " · ") + shelliaVersionBadge(ui))
	fmt.Println(style(ui, colorDim, fmt.Sprintf("%s · %s", ctxInfo.CWD, plainHeaderGitValue(ctxInfo))))
}

// printSection dibuixa una capçalera de secció amb més jerarquia visual.
func printSection(ui bool, title string, color string) {
	fmt.Println()
	fmt.Println(style(ui, color+colorBold, title))
}

// printSessionBanner mostra l'entrada curta de la sessió interactiva.
func printSessionBanner(ui bool) {
	fmt.Println()
	fmt.Println(shelliaBrand(ui, true) + style(ui, colorDim, " session · ") + shelliaVersionBadge(ui))
	fmt.Println(style(ui, colorWhite, "  Interactive mode"))
	fmt.Println(style(ui, colorDim, "  !<cmd>  :shell  :ai  :mode  exit  quit  clear  context"))
}

// printCommandExecution presenta el comando actiu dins d'un bloc de pas abans d'executar-lo.
func printCommandExecution(ui bool, cfg config, index int, total int, plan commandPlan) *stepBox {
	box := newStepBox(os.Stdout, ui, fmt.Sprintf("step %d/%d", index, total))
	box.Bullet(plan.Purpose)
	box.KeyValue("run", plan.Command, colorCyan, colorWhite)
	if plan.Interactive {
		box.KeyValue("interactive", fallbackValue(plan.InteractiveReason, "yes"), colorYellow, colorWhite)
	}
	if cfg.Verbose {
		box.KeyValue("risk", plainRiskLabel(plan.Risk), colorYellow, colorWhite)
	}
	return box
}

// printInfo mostra un missatge informatiu curt.
func printInfo(ui bool, message string) {
	fmt.Printf("%s %s\n", shelliaBrand(ui, false), style(ui, colorWhite+colorBold, message))
}

// printModeStatus mostra un canvi d'estat del mode interactiu amb una sortida més neta.
func printModeStatus(ui bool, message string) {
	renderPanel(os.Stdout, ui, "mode", colorCyan, []string{
		style(ui, colorWhite+colorBold, message),
	})
}

// printWarning mostra un avís no fatal.
func printWarning(ui bool, message string) {
	fmt.Fprintf(os.Stderr, "%s %s\n", style(ui, colorYellow+colorBold, "warning"), style(ui, colorWhite+colorBold, message))
}

// printFinalResult mostra la resposta final útil per a l'usuari (fallback no-streaming).
func printFinalResult(ui bool, message string) {
	fmt.Println()
	fmt.Println(shelliaBrand(ui, false))
	fmt.Printf("%s%s\n", answerPrefix(ui), style(ui, colorWhite+colorBold, strings.TrimSpace(message)))
	fmt.Println()
	fmt.Println(style(ui, colorDim, strings.Repeat("─", boxWidth())))
}

// openResultPanel obre el bloc de resultat en streaming.
func openResultPanel(ui bool) {
	fmt.Println()
	fmt.Println(style(ui, colorDim, strings.Repeat("─", boxWidth())))
	fmt.Println()
	fmt.Println(shelliaBrand(ui, false))
}

// closeResultPanel tanca visualment el resultat en streaming.
func closeResultPanel(ui bool) {
	fmt.Print(styleEnd(ui))
	fmt.Println()
	fmt.Println()
	fmt.Println(style(ui, colorDim, strings.Repeat("─", boxWidth())))
}

// resultWriter wraps os.Stdout to prefix each line with a subtle result indent.
// wroteAnything tracks whether any content has been sent to the terminal,
// regardless of whether lineStart is currently true or false.
type resultWriter struct {
	ui            bool
	lineStart     bool
	wroteAnything bool
}

func (writer *resultWriter) Write(data []byte) (int, error) {
	text := string(data)
	for len(text) > 0 {
		if writer.lineStart {
			if _, err := fmt.Fprint(os.Stdout, answerPrefix(writer.ui), styleStart(writer.ui, colorWhite+colorBold)); err != nil {
				return 0, err
			}
			writer.lineStart = false
			writer.wroteAnything = true
		}

		newlineIndex := strings.IndexByte(text, '\n')
		if newlineIndex == -1 {
			if _, err := fmt.Fprint(os.Stdout, text); err != nil {
				return 0, err
			}
			return len(data), nil
		}

		chunk := text[:newlineIndex]
		if _, err := fmt.Fprint(os.Stdout, chunk, styleEnd(writer.ui), "\n"); err != nil {
			return 0, err
		}
		writer.lineStart = true
		text = text[newlineIndex+1:]
	}
	return len(data), nil
}

type editableRenderState struct {
	rows      int
	cursorRow int
}

type cursorAffinity int

const (
	cursorAffinityForward cursorAffinity = iota
	cursorAffinityBackward
)

// readInteractivePrompt mostra una entrada clara i retorna el text introduït.
func readInteractivePrompt(ui bool, reader *bufio.Reader, mode interactiveMode) (string, error) {
	fmt.Println()
	fmt.Println(promptQuestionLine(ui, mode))
	prompt := promptPrefix(ui, mode)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Print(prompt)
		line, err := reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", err
		}
		return strings.TrimSpace(line), nil
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Print(prompt)
		line, readErr := reader.ReadString('\n')
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return "", readErr
		}
		return strings.TrimSpace(line), nil
	}
	defer term.Restore(fd, state) //nolint:errcheck

	buffer := make([]rune, 0, 128)
	cursor := 0
	affinity := cursorAffinityForward
	single := []byte{0}
	renderState := &editableRenderState{}

	promptWidth := visibleWidth(prompt)
	contentWidth := promptRenderWidth() - promptWidth
	if contentWidth < 1 {
		contentWidth = 1
	}

	renderEditablePrompt(ui, prompt, buffer, cursor, affinity, renderState)

	for {
		_, err := os.Stdin.Read(single)
		if err != nil {
			return "", err
		}

		switch single[0] {
		case '\r', '\n':
			fmt.Print("\r\n")
			return strings.TrimSpace(string(buffer)), nil
		case 3:
			fmt.Print("\r\n")
			return "exit", nil
		case 27:
			exitPrompt, err := applyEscapeSequenceOrExit(fd, &buffer, &cursor, contentWidth, &affinity)
			if err != nil {
				return "", err
			}
			if exitPrompt {
				fmt.Print("\r\n")
				return "exit", nil
			}
		case 127, 8:
			if cursor == 0 || len(buffer) == 0 {
				continue
			}
			buffer = append(buffer[:cursor-1], buffer[cursor:]...)
			cursor--
			affinity = cursorAffinityForward
		default:
			r, err := readInputRune(single[0])
			if err != nil {
				if errors.Is(err, errDiscardRune) {
					continue
				}
				return "", err
			}
			buffer = append(buffer[:cursor], append([]rune{r}, buffer[cursor:]...)...)
			cursor++
			affinity = cursorAffinityForward
		}

		renderEditablePrompt(ui, prompt, buffer, cursor, affinity, renderState)
	}
}

// promptQuestion retorna el text guia segons el mode interactiu actual.
func promptQuestion(mode interactiveMode) string {
	if mode == interactiveModeShell {
		return "Enter a shell command to run."
	}
	return "What do you want Shellia to do?"
}

// promptQuestionLine renderitza la pregunta del prompt amb la marca Shellia integrada.
func promptQuestionLine(ui bool, mode interactiveMode) string {
	if mode == interactiveModeShell {
		return style(ui, colorDim, promptQuestion(mode))
	}

	if !ui {
		return promptQuestion(mode)
	}

	return style(ui, colorDim, "What do you want ") +
		shelliaBrand(ui, false) +
		style(ui, colorDim, " to do?")
}

// promptPrefix renderitza el prefix visual del prompt segons el mode actual.
func promptPrefix(ui bool, mode interactiveMode) string {
	if mode == interactiveModeShell {
		return style(ui, colorWhite+colorBold, "shell") + style(ui, colorWhite, " › ")
	}
	return style(ui, colorCyan+colorBold, "you") + style(ui, colorWhite, " › ")
}

type confirmDecision int

const (
	confirmDecisionCancel confirmDecision = iota
	confirmDecisionRun
	confirmDecisionEdit
	confirmDecisionInteractive
)

// logConfirmationChoice anota la decisió de confirmació a la mateixa línia del prompt original.
func logConfirmationChoice(box *stepBox, prompt string, choice string) {
	if box == nil {
		return
	}
	rendered := style(box.ui, colorYellow+colorBold, "• confirm ") +
		style(box.ui, colorWhite, prompt) +
		style(box.ui, colorYellow+colorBold, choice)
	box.ReplaceLastRenderedRow(rendered)
}

// promptConfirmation demana confirmació explícita dins del bloc del pas.
func promptConfirmation(box *stepBox, reader *bufio.Reader, prompt string, initialCommand string) (confirmDecision, string, error) {
	if box != nil {
		box.KeyValue("confirm", prompt, colorYellow, colorWhite)
	}

	key, ok, err := readSingleConfirmationKey()
	if err == nil && ok {
		for {
			lower := strings.ToLower(string(key))
			switch lower {
			case "y":
				logConfirmationChoice(box, prompt, "yes")
				return confirmDecisionRun, "", nil
			case "e":
				logConfirmationChoice(box, prompt, "edit")
				edited, editErr := box.EditCommand(reader, initialCommand)
				if editErr != nil {
					return confirmDecisionCancel, "", editErr
				}
				if strings.TrimSpace(edited) == "" {
					return confirmDecisionCancel, "", nil
				}
				return confirmDecisionEdit, edited, nil
			case "i":
				logConfirmationChoice(box, prompt, "interactive")
				return confirmDecisionInteractive, "", nil
			case "n":
				logConfirmationChoice(box, prompt, "no")
				return confirmDecisionCancel, "", nil
			}

			key, ok, err = readSingleConfirmationKey()
			if err != nil || !ok {
				break
			}
		}
	}

	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return confirmDecisionCancel, "", err
	}

	answer := strings.ToLower(strings.TrimSpace(line))
	switch answer {
	case "y", "yes":
		logConfirmationChoice(box, prompt, "yes")
		return confirmDecisionRun, "", nil
	case "e", "edit":
		logConfirmationChoice(box, prompt, "edit")
		edited, editErr := box.EditCommand(reader, initialCommand)
		if editErr != nil {
			return confirmDecisionCancel, "", editErr
		}
		if strings.TrimSpace(edited) == "" {
			return confirmDecisionCancel, "", nil
		}
		return confirmDecisionEdit, edited, nil
	case "i", "interactive":
		logConfirmationChoice(box, prompt, "interactive")
		return confirmDecisionInteractive, "", nil
	default:
		logConfirmationChoice(box, prompt, "no")
		return confirmDecisionCancel, "", nil
	}
}

// readSingleConfirmationKey intenta llegir una sola tecla sense esperar Enter.
func readSingleConfirmationKey() (byte, bool, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return 0, false, nil
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		return 0, false, nil
	}
	defer term.Restore(fd, state) //nolint:errcheck

	buffer := []byte{0}
	_, err = os.Stdin.Read(buffer)
	if err != nil {
		return 0, false, err
	}

	return buffer[0], true, nil
}

var errDiscardRune = errors.New("discard rune")

// readInputRune decodifica una rune UTF-8 completa a partir del primer byte ja llegit.
func readInputRune(first byte) (rune, error) {
	if first < utf8.RuneSelf {
		if first < 32 && first != '\t' {
			return 0, errDiscardRune
		}
		return rune(first), nil
	}

	size := utf8SequenceLength(first)
	if size == 0 {
		return 0, errDiscardRune
	}

	buf := make([]byte, size)
	buf[0] = first
	for index := 1; index < size; index++ {
		if _, err := os.Stdin.Read(buf[index : index+1]); err != nil {
			return 0, err
		}
	}

	r, decoded := utf8.DecodeRune(buf)
	if r == utf8.RuneError && decoded == 1 {
		return 0, errDiscardRune
	}
	return r, nil
}

// utf8SequenceLength retorna la longitud esperada d'una seqüència UTF-8 segons el primer byte.
func utf8SequenceLength(first byte) int {
	switch {
	case first&0b1110_0000 == 0b1100_0000:
		return 2
	case first&0b1111_0000 == 0b1110_0000:
		return 3
	case first&0b1111_1000 == 0b1111_0000:
		return 4
	default:
		return 0
	}
}

// wrapPromptRunesWithOffsets parteix el buffer segons l'amplada disponible i
// retorna també l'offset inicial de cada línia dins del buffer original.
// Manté els espais al final de línia perquè el render i el caret comparteixin
// exactament el mateix model visual.
func wrapPromptRunesWithOffsets(buffer []rune, width int) ([]string, []int) {
	if width < 1 {
		width = 1
	}
	if len(buffer) == 0 {
		return []string{""}, []int{0}
	}

	lines := make([]string, 0, len(buffer)/width+1)
	offsets := make([]int, 0, len(buffer)/width+1)
	start := 0
	for start < len(buffer) {
		offsets = append(offsets, start)
		remaining := len(buffer) - start
		if remaining <= width {
			lines = append(lines, string(buffer[start:]))
			break
		}

		lastSpace := -1
		for index := 0; index < width; index++ {
			if unicode.IsSpace(buffer[start+index]) {
				lastSpace = index
			}
		}

		chunkWidth := width
		if lastSpace > 0 {
			chunkWidth = lastSpace + 1
		}

		lines = append(lines, string(buffer[start:start+chunkWidth]))
		start += chunkWidth
	}

	if len(lines) == 0 {
		return []string{""}, []int{0}
	}
	return lines, offsets
}

// promptCursorPosition calcula la fila i la columna del caret dins del layout
// embolicat del prompt a partir dels offsets de cada línia.
// L'afinitat permet representar un límit de línia com a final de la fila
// anterior o com a inici de la següent, segons d'on ve el moviment.
func promptCursorPosition(lines []string, offsets []int, cursor int, affinity cursorAffinity) (int, int) {
	if len(lines) == 0 || len(offsets) == 0 {
		return 0, 0
	}

	if cursor < 0 {
		cursor = 0
	}
	lastRow := len(lines) - 1
	maxCursor := offsets[lastRow] + len([]rune(lines[lastRow]))
	if cursor > maxCursor {
		cursor = maxCursor
	}

	if affinity == cursorAffinityBackward {
		for row := 1; row < len(offsets); row++ {
			if cursor == offsets[row] {
				return row - 1, len([]rune(lines[row-1]))
			}
		}
	}

	row := 0
	for index := len(offsets) - 1; index >= 0; index-- {
		if cursor >= offsets[index] {
			row = index
			break
		}
	}

	col := cursor - offsets[row]
	lineLen := len([]rune(lines[row]))
	if col > lineLen {
		col = lineLen
	}
	return row, col
}

// moveCursorVertical mou el cursor una fila amunt (delta=-1) o avall (delta=+1)
// intentant mantenir la mateixa columna visual.
func moveCursorVertical(buffer []rune, cursor int, contentWidth int, delta int, affinity cursorAffinity) (int, cursorAffinity) {
	lines, offsets := wrapPromptRunesWithOffsets(buffer, contentWidth)
	if len(lines) <= 1 {
		if delta < 0 {
			return 0, cursorAffinityForward
		}
		if delta > 0 {
			return len(buffer), cursorAffinityForward
		}
		return cursor, affinity
	}

	cursorRow, cursorCol := promptCursorPosition(lines, offsets, cursor, affinity)

	targetRow := cursorRow + delta
	if targetRow < 0 {
		return 0, cursorAffinityForward
	}
	if targetRow >= len(lines) {
		return len(buffer), cursorAffinityForward
	}

	col := cursorCol
	if targetLineLen := len([]rune(lines[targetRow])); col > targetLineLen {
		col = targetLineLen
	}

	targetCursor := offsets[targetRow] + col
	targetAffinity := cursorAffinityForward
	if targetRow < len(lines)-1 && col == len([]rune(lines[targetRow])) {
		targetAffinity = cursorAffinityBackward
	}

	return targetCursor, targetAffinity
}

// applyEscapeSequence resol les tecles especials de cursor i edició.
// contentWidth és l'amplada del contingut editable (sense el prefix); si és 0
// el moviment vertical queda desactivat (p. ex. a l'editor d'una sola fila).
func applyEscapeSequence(buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) error {
	sequence := []byte{0, 0}
	if _, err := os.Stdin.Read(sequence[:1]); err != nil {
		return err
	}
	if sequence[0] != '[' {
		return nil
	}
	if _, err := os.Stdin.Read(sequence[1:2]); err != nil {
		return err
	}

	switch sequence[1] {
	case 'A':
		if contentWidth > 0 {
			currentAffinity := cursorAffinityForward
			if affinity != nil {
				currentAffinity = *affinity
			}
			nextCursor, nextAffinity := moveCursorVertical(*buffer, *cursor, contentWidth, -1, currentAffinity)
			*cursor = nextCursor
			if affinity != nil {
				*affinity = nextAffinity
			}
		}
	case 'B':
		if contentWidth > 0 {
			currentAffinity := cursorAffinityForward
			if affinity != nil {
				currentAffinity = *affinity
			}
			nextCursor, nextAffinity := moveCursorVertical(*buffer, *cursor, contentWidth, +1, currentAffinity)
			*cursor = nextCursor
			if affinity != nil {
				*affinity = nextAffinity
			}
		}
	case 'C':
		if *cursor < len(*buffer) {
			*cursor = *cursor + 1
		}
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	case 'D':
		if *cursor > 0 {
			*cursor = *cursor - 1
		}
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	case 'H':
		*cursor = 0
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	case 'F':
		*cursor = len(*buffer)
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	case '3':
		tilde := []byte{0}
		if _, err := os.Stdin.Read(tilde); err != nil {
			return err
		}
		if tilde[0] == '~' && *cursor < len(*buffer) {
			*buffer = append((*buffer)[:*cursor], (*buffer)[*cursor+1:]...)
		}
		if affinity != nil {
			*affinity = cursorAffinityForward
		}
	}

	return nil
}

// applyEscapeSequenceOrExit interpreta una seqüència d'escapament o tanca el prompt si Esc va sol.
func applyEscapeSequenceOrExit(fd int, buffer *[]rune, cursor *int, contentWidth int, affinity *cursorAffinity) (bool, error) {
	ready, err := isInputReady(fd)
	if err != nil {
		return false, err
	}
	if !ready {
		return true, nil
	}
	return false, applyEscapeSequence(buffer, cursor, contentWidth, affinity)
}

// renderEditablePrompt repinta tot el bloc del prompt editable, gestionant bé el wrap.
func renderEditablePrompt(ui bool, prompt string, buffer []rune, cursor int, affinity cursorAffinity, state *editableRenderState) {
	lines, cursorRow, cursorCol := editablePromptLayout(prompt, buffer, cursor, affinity, promptRenderWidth())
	promptWidth := visibleWidth(prompt)

	clearEditablePrompt(state)

	for index, line := range lines {
		if index > 0 {
			fmt.Print("\r\n")
		}
		if index == 0 {
			fmt.Print(prompt)
		} else {
			fmt.Print(strings.Repeat(" ", promptWidth))
		}
		fmt.Print(style(ui, colorWhite, line))
	}

	rows := len(lines)
	rowsBelow := rows - 1 - cursorRow
	fmt.Print("\r")
	if rowsBelow > 0 {
		fmt.Printf("\033[%dA", rowsBelow)
	}
	if cursorCol > 0 {
		fmt.Printf("\033[%dC", cursorCol)
	}

	if state != nil {
		state.rows = rows
		state.cursorRow = cursorRow
	}
}

// clearEditablePrompt neteja totes les files del prompt renderitzat anteriorment.
func clearEditablePrompt(state *editableRenderState) {
	if state == nil || state.rows == 0 {
		return
	}

	fmt.Print("\r")
	if state.cursorRow > 0 {
		fmt.Printf("\033[%dA", state.cursorRow)
	}
	for index := 0; index < state.rows; index++ {
		fmt.Print("\033[2K")
		if index < state.rows-1 {
			fmt.Print("\033[1B\r")
		}
	}
	if state.rows > 1 {
		fmt.Printf("\033[%dA", state.rows-1)
	}
	fmt.Print("\r")
}

// editablePromptLayout calcula les línies visibles i la posició del cursor del prompt editable.
func editablePromptLayout(prompt string, buffer []rune, cursor int, affinity cursorAffinity, width int) ([]string, int, int) {
	if width < 2 {
		width = 2
	}

	promptWidth := visibleWidth(prompt)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(buffer) {
		cursor = len(buffer)
	}

	contentWidth := width - promptWidth
	if contentWidth < 1 {
		contentWidth = 1
	}

	lines, offsets := wrapPromptRunesWithOffsets(buffer, contentWidth)
	cursorRow, contentCol := promptCursorPosition(lines, offsets, cursor, affinity)
	cursorCol := promptWidth + contentCol
	return lines, cursorRow, cursorCol
}

// promptRenderWidth retorna l'amplada útil per al prompt editable evitant l'última columna.
func promptRenderWidth() int {
	fd := int(os.Stdout.Fd())
	if term.IsTerminal(fd) {
		if width, _, err := term.GetSize(fd); err == nil && width > 1 {
			return width - 1
		}
	}
	return 79
}

// wrapPromptRunes parteix el text del prompt en línies, prioritzant salts per
// paraules però mantenint tots els espais del buffer.
func wrapPromptRunes(buffer []rune, width int) []string {
	lines, _ := wrapPromptRunesWithOffsets(buffer, width)
	return lines
}

// clearScreen neteja la terminal actual.
func clearScreen() {
	fmt.Print("\033[2J\033[H")
}

// renderPanel dibuixa un bloc visual lleuger per distingir millor la UI de Shellia.
func renderPanel(target io.Writer, ui bool, title string, color string, lines []string) {
	fmt.Fprintln(target)
	fmt.Fprintln(target, style(ui, color+colorBold, title))
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			fmt.Fprintln(target)
			continue
		}
		for _, part := range strings.Split(line, "\n") {
			fmt.Fprintf(target, "%s%s\n", style(ui, colorDim, "  "), part)
		}
	}
}

// plainHeaderGitValue genera un resum breu de Git sense estils.
func plainHeaderGitValue(ctxInfo contextInfo) string {
	if !ctxInfo.Git.IsRepo {
		return "not a repository"
	}

	branch := fallbackValue(ctxInfo.Git.Branch, "DETACHED")
	if strings.TrimSpace(ctxInfo.Git.StatusShort) == "" {
		return branch + " (clean)"
	}
	return branch + " (dirty)"
}

// shellStreamPrefix retorna el prefix usat per a cada línia de sortida real del shell.
func shellStreamPrefix(ui bool) string {
	return style(ui, colorDim, "│   ")
}

// answerPrefix retorna el marge visual per a la resposta principal de Shellia.
func answerPrefix(ui bool) string {
	return style(ui, colorGreen, "  ")
}

// shelliaBrand renderitza la marca Shellia amb "ia" accentuat.
func shelliaBrand(ui bool, lower bool) string {
	left := "Shell"
	if lower {
		left = "shell"
	}
	return style(ui, colorWhite+colorBold, left) + style(ui, colorCyan+colorBold, "ia")
}

// shelliaVersionBadge renderitza la versió actual amb un tractament visual compacte.
func shelliaVersionBadge(ui bool) string {
	return badge(ui, colorCyan, fallbackValue(strings.TrimSpace(version), "dev"))
}

// plainRiskLabel retorna el nivell de risc sense estils ANSI.
func plainRiskLabel(risk string) string {
	switch risk {
	case riskSafe:
		return "safe"
	case riskHigh:
		return "high"
	default:
		return "medium"
	}
}

// style aplica estils ANSI quan la sortida els suporta.
func style(ui bool, color string, text string) string {
	if !ui {
		return text
	}
	return color + text + colorReset
}

// styleStart returns only the ANSI prefix for a style.
func styleStart(ui bool, color string) string {
	if !ui {
		return ""
	}
	return color
}

// styleEnd returns the ANSI reset sequence when UI styling is enabled.
func styleEnd(ui bool) string {
	if !ui {
		return ""
	}
	return colorReset
}

// badge genera una etiqueta visual compacta per a la interfície.
func badge(ui bool, color string, text string) string {
	return style(ui, color+colorBold, text)
}

// metaLabel renderitza una etiqueta de metadades lleugera.
func metaLabel(ui bool, text string) string {
	return style(ui, colorDim, strings.ToLower(text))
}

// metaLine renderitza metadades compactes clau/valor.
func metaLine(ui bool, key string, value string) string {
	return fmt.Sprintf("%s %s", metaLabel(ui, key), value)
}

// stepBadge genera una etiqueta compacta per a cada pas del pla.
func stepBadge(ui bool, step int) string {
	return style(ui, colorBlue+colorBold, fmt.Sprintf("%d.", step))
}

// riskBadge renders the risk level with a colour-coded label.
func riskBadge(ui bool, risk string) string {
	switch risk {
	case riskSafe:
		return badge(ui, colorGreen, "safe")
	case riskHigh:
		return badge(ui, colorRed, "high")
	default:
		return badge(ui, colorYellow, "medium")
	}
}

// classificationBadge renders the local command classification.
func classificationBadge(ui bool, classification string) string {
	switch classification {
	case classificationSafe:
		return badge(ui, colorGreen, "safe")
	case classificationDangerous:
		return badge(ui, colorRed, "dangerous")
	default:
		return badge(ui, colorYellow, "risky")
	}
}

// confirmBadge indica clarament si el comando requereix confirmació.
func confirmBadge(ui bool, required bool) string {
	if required {
		return badge(ui, colorYellow, "required")
	}
	return badge(ui, colorGreen, "auto")
}

// fallbackValue retorna un text alternatiu quan el valor és buit.
func fallbackValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// indentLines sagna un bloc multilínia per presentar-lo millor en terminal.
func indentLines(text string, prefix string) string {
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		lines[index] = prefix + line
	}
	return strings.Join(lines, "\n")
}

// isRetryInstruction detecta quan l'usuari vol repetir l'últim intent no completat.
func isRetryInstruction(input string) bool {
	normalized := strings.ToLower(strings.TrimSpace(input))
	switch normalized {
	case "again", "retry", "try again", "do it again", "torna-ho a provar", "torna ho a provar", "fes-ho de nou", "fes ho de nou":
		return true
	default:
		return false
	}
}

// trimTrailingBlankLines elimina línies en blanc sobrants al final d'un bloc.
func trimTrailingBlankLines(lines []string) []string {
	last := len(lines) - 1
	for last >= 0 && strings.TrimSpace(lines[last]) == "" {
		last--
	}
	if last < 0 {
		return nil
	}
	return lines[:last+1]
}

// boxMinWidth garanteix una amplada mínima llegible fins i tot en terminals estrets.
const boxMinWidth = 40

// boxHorizontalMargin deixa una mica d'aire a la dreta del bloc.
const boxHorizontalMargin = 4

// boxWidth calcula l'amplada total del bloc de pas segons la mida actual de la terminal.
func boxWidth() int {
	fd := int(os.Stdout.Fd())
	if term.IsTerminal(fd) {
		width, _, err := term.GetSize(fd)
		if err == nil && width > 0 {
			width -= boxHorizontalMargin
			if width < boxMinWidth {
				return boxMinWidth
			}
			return width
		}
	}
	return 80
}

// visibleWidth calcula l'amplada visible d'una cadena ignorant les seqüències ANSI.
// Les seqüències CSI tenen la forma \033[ <params> <final>, on el byte final és 0x40–0x7E.
// El caràcter '[' (0x5B) és l'introductor CSI i NO s'ha de tractar com a byte final.
func visibleWidth(text string) int {
	width := 0
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		r := runes[i]
		if r == '\033' && i+1 < len(runes) && runes[i+1] == '[' {
			// Seqüència CSI: \033[ ... byte_final (0x40–0x7E)
			i += 2 // salta \033 i [
			for i < len(runes) && !(runes[i] >= '@' && runes[i] <= '~') {
				i++
			}
			i++ // salta el byte final
			continue
		}
		if r == '\033' {
			i++ // salta ESC solitari
			continue
		}
		width++
		i++
	}
	return width
}
