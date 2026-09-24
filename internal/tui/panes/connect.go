package panes

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/colbytimm/alchemist/internal/theme"
)

const (
	connectTitle   = "Connect"
	connectHint    = "enter connect · tab next field · space toggle · esc "
	connectingText = "Connecting…"
	labelWidth     = 10
)

// What the form refuses to submit without, worded the way the fields are
// labeled, since the message sits right under them.
var (
	errNoProfile  = errors.New("name the profile")
	errNoEndpoint = errors.New("enter the account endpoint")
	errNoKey      = errors.New("enter the account key")
)

// ConnectForm is what the connect form collects: a profile to create or
// complete, its key, and whether to remember the key.
type ConnectForm struct {
	Profile    string
	Endpoint   string
	Key        string
	SkipVerify bool
	StoreKey   bool
}

// connectField is one row of the form, in tab order. The text fields come
// first and index the inputs.
type connectField int

const (
	fieldProfile connectField = iota
	fieldEndpoint
	fieldKey
	fieldSkipVerify
	fieldStoreKey
	fieldCount
)

const textFieldCount = 3

var fieldLabels = [fieldCount]string{
	fieldProfile:    "Profile",
	fieldEndpoint:   "Endpoint",
	fieldKey:        "Key",
	fieldSkipVerify: "Skip TLS verification (the emulator's self-signed certificate)",
	fieldStoreKey:   "Remember the key in the OS keychain",
}

var fieldPlaceholders = [textFieldCount]string{"emulator", "https://localhost:8081", "account key"}

// Connect is the form that connects an account and saves it as a profile.
// Like the inputs it wraps, its value receiver hides shared slices, so a
// caller must keep every Connect it is handed.
type Connect struct {
	frame      frame
	icons      theme.IconSet
	intro      string
	escape     string
	inputs     [textFieldCount]textinput.Model
	skipVerify bool
	storeKey   bool
	focus      connectField
	spinner    spinner.Model
	connecting bool
	failure    string
}

// NewConnect seeds the form and puts the focus on the first field still
// empty, so a profile that only lacks its key opens on the key.
func NewConnect(icons theme.IconSet, form ConnectForm) Connect {
	c := Connect{
		frame:  frame{title: connectTitle, focused: true},
		icons:  icons,
		intro:  intro(form),
		escape: "quit",
		spinner: spinner.New(
			spinner.WithSpinner(spinner.Spinner{Frames: icons.SpinnerFrames, FPS: spinnerFPS}),
			spinner.WithStyle(theme.SpinnerStyle()),
		),
		skipVerify: form.SkipVerify,
		storeKey:   form.StoreKey,
	}
	values := [textFieldCount]string{form.Profile, form.Endpoint, form.Key}
	for i := range c.inputs {
		c.inputs[i] = newInput(fieldPlaceholders[i], values[i])
	}
	c.inputs[fieldKey].EchoMode = textinput.EchoPassword
	return c.setFocus(c.firstEmptyField())
}

// intro says why the screen is up: a seeded endpoint means a profile that
// exists but has no key anywhere.
func intro(form ConnectForm) string {
	if form.Endpoint != "" {
		return fmt.Sprintf("Profile %s has no key in the keychain or the environment. Enter it to connect.", form.Profile)
	}
	return "No profile yet. Enter the account to connect to; the profile is saved to config.toml, the key never is."
}

// OverSession marks a form opened over a running session: esc goes back rather than quitting.
func (c Connect) OverSession() Connect {
	c.escape = "back"
	if c.inputs[fieldEndpoint].Value() == "" {
		c.intro = "Add an account. The profile is saved to config.toml, the key never is."
	}
	return c
}

func newInput(placeholder, value string) textinput.Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = placeholder
	input.PlaceholderStyle = theme.HintStyle()
	input.TextStyle = theme.TextStyle()
	// A static cursor stays visible without a blink timer waking the program
	// twice a second.
	input.Cursor.SetMode(cursor.CursorStatic)
	input.SetValue(value)
	return input
}

func (c Connect) firstEmptyField() connectField {
	for i := range c.inputs {
		if strings.TrimSpace(c.inputs[i].Value()) == "" {
			return connectField(i)
		}
	}
	return fieldKey
}

// Update advances the animation and routes typing to the focused field. On a
// checkbox, space toggles it instead.
func (c Connect) Update(msg tea.Msg) (Connect, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		return c.animate(msg)
	case tea.KeyMsg:
		return c.typeKey(msg)
	}
	return c, nil
}

func (c Connect) animate(tick spinner.TickMsg) (Connect, tea.Cmd) {
	if !c.connecting {
		return c, nil
	}
	var cmd tea.Cmd
	c.spinner, cmd = c.spinner.Update(tick)
	return c, cmd
}

func (c Connect) typeKey(msg tea.KeyMsg) (Connect, tea.Cmd) {
	if c.focus >= textFieldCount {
		if msg.String() == " " {
			return c.toggle(), nil
		}
		return c, nil
	}
	var cmd tea.Cmd
	c.inputs[c.focus], cmd = c.inputs[c.focus].Update(msg)
	return c, cmd
}

func (c Connect) toggle() Connect {
	switch c.focus {
	case fieldSkipVerify:
		c.skipVerify = !c.skipVerify
	case fieldStoreKey:
		c.storeKey = !c.storeKey
	}
	return c
}

func (c Connect) NextField() Connect {
	return c.setFocus((c.focus + 1) % fieldCount)
}

func (c Connect) PrevField() Connect {
	return c.setFocus((c.focus + fieldCount - 1) % fieldCount)
}

// setFocus moves the keyboard to field. The inputs' own focus command only
// drives a blinking cursor, and these are static.
func (c Connect) setFocus(field connectField) Connect {
	for i := range c.inputs {
		c.inputs[i].Blur()
	}
	c.focus = field
	if field < textFieldCount {
		c.inputs[field].Focus()
	}
	return c
}

// Form returns what was typed, or the first thing still missing.
func (c Connect) Form() (ConnectForm, error) {
	form := ConnectForm{
		Profile:    strings.TrimSpace(c.inputs[fieldProfile].Value()),
		Endpoint:   strings.TrimSpace(c.inputs[fieldEndpoint].Value()),
		Key:        strings.TrimSpace(c.inputs[fieldKey].Value()),
		SkipVerify: c.skipVerify,
		StoreKey:   c.storeKey,
	}
	switch {
	case form.Profile == "":
		return ConnectForm{}, errNoProfile
	case form.Endpoint == "":
		return ConnectForm{}, errNoEndpoint
	case form.Key == "":
		return ConnectForm{}, errNoKey
	}
	return form, nil
}

// Connecting reports whether an attempt is in flight.
func (c Connect) Connecting() bool {
	return c.connecting
}

// StartConnecting shows the attempt in progress. The returned command starts
// its animation.
func (c Connect) StartConnecting() (Connect, tea.Cmd) {
	c.connecting = true
	c.failure = ""
	return c, c.spinner.Tick
}

// Fail shows why the attempt did not connect and hands the form back.
func (c Connect) Fail(err error) Connect {
	c.connecting = false
	c.failure = err.Error()
	return c
}

func (c Connect) SetSize(width, height int) Connect {
	c.frame = c.frame.size(width, height)
	return c
}

func (c Connect) View() string {
	width, _ := c.frame.inner()
	lines := []string{theme.TextStyle().Width(width).Render(c.intro), ""}
	for field := connectField(0); field < fieldCount; field++ {
		lines = append(lines, c.row(field))
	}
	lines = append(lines, "", c.status(width), "", theme.HintStyle().Render(connectHint+c.escape))
	return c.frame.render(strings.Join(lines, "\n"))
}

func (c Connect) row(field connectField) string {
	label := theme.TextStyle()
	if field == c.focus {
		label = theme.SelectedStyle()
	}
	if field < textFieldCount {
		return label.Width(labelWidth).Render(fieldLabels[field]) + c.inputs[field].View()
	}
	return label.Render(c.checkbox(field) + " " + fieldLabels[field])
}

func (c Connect) checkbox(field connectField) string {
	checked := c.skipVerify
	if field == fieldStoreKey {
		checked = c.storeKey
	}
	if checked {
		return "[x]"
	}
	return "[ ]"
}

// status is the line under the fields: the attempt in progress, the last
// failure wrapped to fit, or nothing.
func (c Connect) status(width int) string {
	switch {
	case c.connecting:
		return c.spinner.View() + " " + theme.TextStyle().Render(connectingText)
	case c.failure != "":
		return theme.ErrorStyle().Width(width).Render(c.icons.Failure + " " + c.failure)
	}
	return ""
}
