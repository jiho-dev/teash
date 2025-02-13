package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/willgorman/teash/iterm2"
	"golang.org/x/exp/maps"
)

// TODO: (willgorman)
//   - fix columns selection for labels
//   - ranking still seems weird. levenstein distance can be the same for two results
//     where one has a prefix of the search term and one does not but the one without
//     the prefix may be shown first...
//   - remove / from search input
//   - rank the rows by best match?
var baseStyle = lipgloss.NewStyle().
	BorderStyle(lipgloss.NormalBorder()).
	BorderForeground(lipgloss.Color("240"))

var (
	helpStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render
	loadingStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	tsh          string
)

type Teleport interface {
	GetNodes(refresh bool) (Nodes, error)
	GetCluster() (string, error)
	Connect(cmd []string)
}

type model struct {
	table         table.Model
	search        textinput.Model
	teleport      Teleport
	tshCmd        []string
	nodes         Nodes
	visible       Nodes
	searching     bool
	columnSelMode bool
	columnSel     int
	colFilters    map[int]string
	headers       map[int]string
	profile       string
	spinner       spinner.Model
}

// Init is the first function that will be called. It returns an optional
// initial command. To not perform an initial command return nil.
func (m model) Init() tea.Cmd {
	// TODO: (willgorman) cursor blink?
	return tea.Batch(func() tea.Msg {
		nodes, err := m.teleport.GetNodes(false)
		if err != nil {
			return err
		}
		return nodes
	}, m.spinner.Tick)
}

func (m model) toggleColumnFilter(idx int, val string) {
	o := m.colFilters[idx]
	if val == o {
		delete(m.colFilters, idx)
	} else {
		m.colFilters[idx] = val
	}
}

// Update is called when a message is received. Use it to inspect messages
// and, in response, update the model and/or send a command.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	// log.Printf("table focus: %t search focus: %t\n", m.table.Focused(), m.search.Focused())
	if m.searching {
		m.search.Focus()
	}

	// command window
	marginH := int(10)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.table.SetHeight(msg.Height - marginH)
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.search.Focused() {
				// log.Println("SEARCH FOCUS -> BLUR")
				m.search.Blur()
			}
			if !m.table.Focused() {
				// log.Println("TABLE BLUR -> FOCUS")
				m.table.Focus()
			}

			m.search.SetValue("")
			m.searching = false
			m.columnSel = 0
			m.search.Prompt = "> "
			m.columnSelMode = false
			m.colFilters = map[int]string{}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			// TODO: (willgorman) this should be only numbers up to the number of columns
			// and not sure what to do if more than 9 columns
			// Issue #14 should help since using arrows to select columns is easier than a mode
			if m.columnSelMode && m.columnSel == 0 {
				col, _ := strconv.Atoi(msg.String()) // ignore error since we know it's a number
				m.columnSel = col
				m.searching = true

				//log.Println(litter.Sdump("WTF", m.headers))
				m.search.Prompt = m.headers[col-1] + "> "
			}
		case "C":
			if !m.searching {
				m.columnSelMode = true
			}

			// env
		case "D":
			if !m.searching {
				m.toggleColumnFilter(2, "dev")
			}
		case "S":
			if !m.searching {
				m.toggleColumnFilter(2, "stg")
			}
		case "P":
			if !m.searching {
				m.toggleColumnFilter(2, "ppd")
			}
			// Type
		case "c":
			if !m.searching {
				m.toggleColumnFilter(5, "compute")
			}
		case "p":
			if !m.searching {
				m.toggleColumnFilter(5, "platform")
			}

		case "q":
			if !m.searching {
				return m, tea.Quit
			}
		case "ctrl+c":
			return m, tea.Quit
		case "ctrl+r":
			// forcefully refresh cach
			fmt.Printf("Create cachefile ... \n")
			nodes, err := m.teleport.GetNodes(true)
			if err != nil {
			} else {
				m.nodes = nodes
			}

		case "/":
			m.searching = true
			// we want to focus to activate the cursor but we don't want
			// it to handle this message since that adds '/' to the value
			return m, m.search.Focus()
		case "enter":
			m.tshCmd = []string{"tsh", "ssh", m.table.SelectedRow()[2]}
			return m, tea.Quit
		}
	case Nodes:
		m.nodes = Nodes(msg)
		m.visible = m.nodes
		return m.fillTable(), nil
	case error:
		panic(msg)
	}

	// log.Println(litter.Sdump(msg))
	m.search, _ = m.search.Update(msg)
	m = m.filterNodesBySearch().fillTable()
	m.spinner, cmd = m.spinner.Update(msg)
	cmds = append(cmds, cmd)
	m.table, cmd = m.table.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View renders the program's UI, which is just a string. The view is
// rendered after every Update.
func (m model) View() string {
	if len(m.tshCmd) > 0 {
		return ""
	}
	return baseStyle.Render(m.table.View()) + m.navView() + "\n" + m.search.View() + "\n" + m.helpView()
}

func (m model) fillTable() model {
	// labelCols := map[string]int{}
	labelSet := map[string]struct{}{}
	// labelIdx := 2
	for _, n := range m.nodes {
		for l := range n.Labels {
			labelSet[l] = struct{}{}
		}
	}
	labels := maps.Keys(labelSet)
	slices.Sort(labels)

	m.headers = map[int]string{
		0: "Region",
		1: "Env",
		2: "Hostname",
		3: "IP",
		4: "Type",
		5: "OS",
	}

	hdrLen := len(m.headers)

	for i, l := range labels {
		m.headers[i+hdrLen] = l
	}

	columns := make([]table.Column, len(labels)+hdrLen)
	width := []int{
		7,  // Region
		5,  // Env
		30, // hostname
		16, // IP
		15, // Type
		30, // OS
	}

	for i, w := range width {
		columns[i] = table.Column{Title: m.title(m.headers[i], i+1), Width: w}
	}

	for i, v := range labels {
		columns[i+hdrLen] = table.Column{Title: m.title(v, i+hdrLen+1), Width: 15}
	}

	// TODO: (willgorman) calculate widths by largest value in the column.  but what's the
	// ideal max width?
	m.table.SetColumns(columns)
	rows := []table.Row{}
	// log.Println("VISIBLE: ", len(m.visible), " ALL: ", len(m.nodes))
	for _, n := range m.visible {
		row := make(table.Row, len(labels)+hdrLen)
		row[0] = n.Region
		row[1] = n.Env
		row[2] = n.Hostname
		row[3] = n.IP
		row[4] = n.NodeType
		row[5] = n.OS
		for l, v := range n.Labels {
			idx := slices.Index(labels, l)
			if idx < 0 {
				continue
			}
			row[idx+hdrLen] = v
		}
		rows = append(rows, row)
	}

	m.table.SetRows(rows)
	// log.Println("TABLE ROWS: ", len(rows))
	// if len(rows) > 0 {
	// 	log.Println("FIRST: ", rows[0][0])
	// }

	if len(m.table.Rows()) == 0 {
		m.table.SetCursor(0)
	}

	if m.table.Cursor() < 0 && len(m.table.Rows()) > 0 {
		m.table.SetCursor(0)
	}

	if m.table.Cursor() >= len(m.table.Rows())-1 {
		m.table.GotoBottom()
	}
	// log.Println("CURSOR: ", m.table.Cursor())
	// log.Println("VISIBLE: ", len(m.visible))

	return m
}

func (m model) title(s string, i int) string {
	if m.columnSelMode {
		return strconv.Itoa(i)
	}
	return s
}

func (m model) applyFilter() []Node {
	visible := m.nodes

	for k, f := range m.colFilters {
		var nodes []Node
		for _, n := range visible {
			switch k {
			case 2: // env
				if strings.HasPrefix(n.Env, f) {
					nodes = append(nodes, n)
				}
			case 5: // type
				if strings.HasPrefix(n.NodeType, f) {
					nodes = append(nodes, n)
				}
			}
		}

		visible = nodes
	}

	return visible
}

func (m model) sortNodes(nodes []Node) []Node {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Env != nodes[j].Env {
			return nodes[i].Env < nodes[j].Env
		}

		if nodes[i].NodeType != nodes[j].NodeType {
			return nodes[i].NodeType < nodes[j].NodeType
		}

		if nodes[i].OS != nodes[j].OS {
			return nodes[i].OS < nodes[j].OS
		}

		return nodes[i].Hostname < nodes[j].Hostname
	})

	return nodes
}

func (m model) filterNodesBySearch() model {
	visible := m.applyFilter()

	defer func() {
		m.visible = m.sortNodes(m.visible)
	}()

	if m.search.Value() == "" {
		m.visible = visible
		return m
	}

	m.visible = nil

	if m.columnSel == 0 {
		txt2node := map[string]Node{}
		// if no column is selected we'll fuzzy search on all columns
		for _, n := range visible {
			allText := n.Hostname + " " + n.IP + " " + n.OS
			// these can't be in random map key order because otherwise
			// the search results will be different
			labels := sort.StringSlice(maps.Keys(n.Labels))
			labels.Sort()
			for _, l := range labels {
				allText = allText + " " + n.Labels[l]
			}
			txt2node[strings.ToLower(allText)] = n
		}
		sortedNodes := sort.StringSlice(maps.Keys(txt2node))
		sortedNodes.Sort()
		// log.Println("SEARCHING: ", m.search.Value(), "IN: ", litter.Sdump(sortedNodes))
		ranks := fuzzy.RankFind(strings.ToLower(m.search.Value()), sortedNodes)
		sort.Sort(ranks)
		for _, rank := range ranks {
			m.visible = append(m.visible, txt2node[rank.Target])
		}
		return m
	}

	txt2nodes := map[string][]Node{}
	for _, n := range visible {
		switch m.columnSel {
		case 1:
			txt2nodes[strings.ToLower(n.Region)] = append(txt2nodes[strings.ToLower(n.Region)], n)
		case 2:
			txt2nodes[strings.ToLower(n.Env)] = append(txt2nodes[strings.ToLower(n.Env)], n)
		case 3:
			txt2nodes[strings.ToLower(n.Hostname)] = append(txt2nodes[strings.ToLower(n.Hostname)], n)
		case 4:
			txt2nodes[strings.ToLower(n.IP)] = append(txt2nodes[strings.ToLower(n.IP)], n)
		case 5:
			txt2nodes[strings.ToLower(n.NodeType)] = append(txt2nodes[strings.ToLower(n.NodeType)], n)
		case 6:
			txt2nodes[strings.ToLower(n.OS)] = append(txt2nodes[strings.ToLower(n.OS)], n)
		default:
			txt2nodes[strings.ToLower(n.Labels[m.headers[m.columnSel-1]])] = append(txt2nodes[strings.ToLower(n.Labels[m.headers[m.columnSel-1]])], n)
		}
	}

	// log.Println("SEARCHING: ", m.search.Value(), "IN: ", litter.Sdump(maps.Keys(txt2nodes)))
	sortedNodes := sort.StringSlice(maps.Keys(txt2nodes))
	sortedNodes.Sort()
	ranks := fuzzy.RankFind(strings.ToLower(m.search.Value()), sortedNodes)
	sort.Sort(ranks)
	// log.Println("RESULTS: ", litter.Sdump(ranks))
	for _, rank := range ranks {
		nodes := txt2nodes[rank.Target]
		for _, n := range nodes {
			m.visible = append(m.visible, n)
		}

	}

	return m
}

func (m model) filterNodesBySearch1() model {
	if m.search.Value() == "" {
		m.visible = m.nodes
		return m
	}
	m.visible = nil

	if m.columnSel == 0 {
		txt2node := map[string]Node{}
		// if no column is selected we'll fuzzy search on all columns
		for _, n := range m.nodes {
			allText := n.Env + " " + n.NodeType + " " + n.Hostname + " " + n.IP + " " + n.OS
			// these can't be in random map key order because otherwise
			// the search results will be different
			labels := sort.StringSlice(maps.Keys(n.Labels))
			labels.Sort()
			for _, l := range labels {
				allText = allText + " " + n.Labels[l]
			}
			txt2node[strings.ToLower(allText)] = n
		}
		sortedNodes := sort.StringSlice(maps.Keys(txt2node))
		sortedNodes.Sort()
		//log.Println("SEARCHING: ", m.search.Value(), "IN: ", litter.Sdump(sortedNodes))
		ranks := fuzzy.RankFind(strings.ToLower(m.search.Value()), sortedNodes)
		sort.Sort(ranks)
		for _, rank := range ranks {
			m.visible = append(m.visible, txt2node[rank.Target])
		}
		return m
	}

	txt2nodes := map[string][]Node{}
	for _, n := range m.nodes {
		switch m.columnSel {
		case 1:
			txt2nodes[strings.ToLower(n.Region)] = append(txt2nodes[strings.ToLower(n.Region)], n)
		case 2:
			txt2nodes[strings.ToLower(n.Env)] = append(txt2nodes[strings.ToLower(n.Env)], n)
		case 3:
			txt2nodes[strings.ToLower(n.Hostname)] = append(txt2nodes[strings.ToLower(n.Hostname)], n)
		case 4:
			txt2nodes[strings.ToLower(n.IP)] = append(txt2nodes[strings.ToLower(n.IP)], n)
		case 5:
			txt2nodes[strings.ToLower(n.NodeType)] = append(txt2nodes[strings.ToLower(n.NodeType)], n)
		case 6:
			txt2nodes[strings.ToLower(n.OS)] = append(txt2nodes[strings.ToLower(n.OS)], n)
		default:
			txt2nodes[strings.ToLower(n.Labels[m.headers[m.columnSel-1]])] = append(txt2nodes[strings.ToLower(n.Labels[m.headers[m.columnSel-1]])], n)
		}
	}

	// log.Println("SEARCHING: ", m.search.Value(), "IN: ", litter.Sdump(maps.Keys(txt2nodes)))
	sortedNodes := sort.StringSlice(maps.Keys(txt2nodes))
	sortedNodes.Sort()
	ranks := fuzzy.RankFind(strings.ToLower(m.search.Value()), sortedNodes)
	sort.Sort(ranks)
	// log.Println("RESULTS: ", litter.Sdump(ranks))
	for _, rank := range ranks {
		nodes := txt2nodes[rank.Target]
		for _, n := range nodes {
			m.visible = append(m.visible, n)
		}

	}
	return m
}

func (m model) navView() string {
	view := fmt.Sprintf("\n[%s]", m.profile)
	// TODO: (willgorman) might be better to have a flag that lasts until the init cmd is done
	// otherwise we'll still show loading when the initial load is done but 0 nodes are in the cluster
	if len(m.nodes) == 0 {
		return view + loadingStyle.Render(" Loading") + m.spinner.View()
	}
	if len(m.visible) != len(m.nodes) {
		return view + fmt.Sprintf(" %d/%d (total: %d)", m.table.Cursor()+1, len(m.visible), len(m.nodes))
	}
	// log.Printf("cursor: %d,  len(m.visible): %d", m.table.Cursor(), len(m.visible))
	return view + fmt.Sprintf(" %d/%d", m.table.Cursor()+1, len(m.nodes))
}

func (m model) helpView() string {
	if m.searching {
		return helpStyle("\n  Type to search • Esc: cancel search • Enter: ssh to selection\n")
	}

	if m.columnSelMode {
		return helpStyle("\n  ↑/↓: Navigate • 0-9: Choose column • q: Quit • Esc: cancel column select • Enter: ssh to selection\n")
	}

	//return helpStyle("\n  ↑/↓: Navigate • /: Start search • q: Quit • c: Select column to search • Enter: ssh to selection\n")

	//help := "↑/↓: Navigate • /: Start search • q: Quit • c: Select column to search • Enter: ssh to selection\n"
	help := "\n"
	help += "  ↑/↓: Navigate • /: Start search • q: Quit • C: Select column to search • Enter: ssh to selection\n"
	help += "  D/S/P: Toggle Env(dev/stg/ppd) • c/p: Toggle Type(compute/platform)\n"

	return helpStyle(help)
}

func (m model) ColumnIndex(name string) int {
	for k, v := range m.headers {
		if v == name {
			return k
		}
	}

	return -1
}

// show Badge and tab-title of iTerms
func (m model) SetIterm2Badge(cfg *Config) {
	if !cfg.Iterm2.Badge.Enable {
		return
	}

	bcfg := &cfg.Iterm2.Badge
	row := m.table.SelectedRow()

	var badge string

	for _, name := range bcfg.Column {
		idx := m.ColumnIndex(name)
		if idx != -1 && idx < len(row) {
			if badge != "" {
				badge += ":"
			}

			badge += row[idx]
		}
	}

	if badge != "" {
		iterm2.PrintBadge(badge)
	}
}

func (m model) SetIterm2TabTitle(cfg *Config) {
	if !cfg.Iterm2.Tab.Enable {
		return
	}

	tcfg := &cfg.Iterm2.Tab
	row := m.table.SelectedRow()

	idx := m.ColumnIndex(tcfg.Title)
	if idx != -1 {
		title := row[idx]

		iterm2.PrintHostName()
		iterm2.PrintTabTitle(title)
		iterm2.PrintRemoteHostName(title)
	}

	var c iterm2.RgbColor

	nodeType := m.table.SelectedRow()[1]
	v, ok := cfg.Iterm2.Tab.Colors[nodeType]
	if !ok {
		v, _ = cfg.Iterm2.Tab.Colors["default"]
	}

	if v != 0 {
		c.Red = int((v & 0xff0000) >> 16)
		c.Green = int((v & 0x00ff00) >> 8)
		c.Blue = int(v & 0x0000ff)

		iterm2.PrintTabBGColor(c)
	}
}

func (m model) ResetIterm2Badge() {
	iterm2.PrintBadge("")
}

func (m model) ResetIterm2TabTitle() {
	iterm2.PrintRemoteHostName("")
	iterm2.PrintTabTitle("")
	iterm2.PrintResetTabBGColor()
}

func generateNodeCache(cfg *Config, tp Teleport) {
	fmt.Printf("Generating Node cachefile: %v \n", cfg.NodeCacheFile)
	tp.GetNodes(false)
	return
}

func DefaultKeyMap() table.KeyMap {
	//const spacebar = " "
	return table.KeyMap{
		LineUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		LineDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("ctrl+b"),
			key.WithHelp("ctrl+b", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("ctrl+f"),
			key.WithHelp("ctrl+f", "page down"),
		),
		/*
			PageUp: key.NewBinding(
				key.WithKeys("b", "pgup"),
				key.WithHelp("b/pgup", "page up"),
			),
			PageDown: key.NewBinding(
				key.WithKeys("f", "pgdown", spacebar),
				key.WithHelp("f/pgdn", "page down"),
			),
			HalfPageUp: key.NewBinding(
				key.WithKeys("u", "ctrl+u"),
				key.WithHelp("u", "½ page up"),
			),
			HalfPageDown: key.NewBinding(
				key.WithKeys("d", "ctrl+d"),
				key.WithHelp("d", "½ page down"),
			),
			GotoTop: key.NewBinding(
				key.WithKeys("home", "g"),
				key.WithHelp("g/home", "go to start"),
			),
			GotoBottom: key.NewBinding(
				key.WithKeys("end", "G"),
				key.WithHelp("G/end", "go to end"),
			),
		*/
	}
}

func main() {
	var configFile string
	var connect string
	var genNodeCache bool

	flag.StringVar(&configFile, "config", "./teash.yaml", "config file path")
	flag.StringVar(&connect, "connect", "", "connect the host")
	flag.BoolVar(&genNodeCache, "nodecache", false, "generate node cache file")
	flag.Parse()

	cfg := readConfig(configFile)
	teleport, err := NewTeleport(cfg, connect)
	if err != nil {
		panic(err)
	}

	// make sure there's at least one profile in teleport,
	// if so then it will use that automatically, otherwise
	// user needs to login first
	profile, err := teleport.GetCluster()
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}

	if genNodeCache {
		fmt.Printf("Create cachefile ... \n")
		teleport.GetNodes(true)
		return
	}

	f, err := tea.LogToFile("debug.log", "debug")
	if err != nil {
		panic(err)
	}

	log.Println("------------------------------------")
	defer func() {
		f.Close()
	}()

	t := table.New(
		table.WithFocused(true),
		//table.WithHeight(20),
	)
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(false)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	t.KeyMap = DefaultKeyMap()

	search := textinput.New()
	spin := spinner.New(
		spinner.WithSpinner(spinner.Ellipsis),
		spinner.WithStyle(loadingStyle))

	md := model{
		table:    t,
		search:   search,
		profile:  profile,
		spinner:  spin,
		teleport: teleport,
	}

	var m tea.Model

	// connectd the host immediately
	if connect != "" {
		nn, err := teleport.GetNodes(false)
		if err != nil {
			panic(err)
		}
		md.nodes = nn
		md.visible = nn

		m1 := md.fillTable()
		m1.table.SetCursor(0)
		m1.tshCmd = []string{"tsh", "ssh", connect}
		m = m1
	} else {
		// env
		md.colFilters = map[int]string{}
		//md.colFilters[2] = "dev"
		//md.colFilters[5] = "compute"

		m, err = tea.NewProgram(md).Run()
		if err != nil {
			panic(err)
		}
	}

	model := m.(model)
	if len(model.tshCmd) == 0 {
		return
	}

	model.SetIterm2Badge(cfg)
	model.SetIterm2TabTitle(cfg)
	defer func() {
		model.ResetIterm2Badge()
		model.ResetIterm2TabTitle()
	}()

	teleport.Connect(model.tshCmd)
}
