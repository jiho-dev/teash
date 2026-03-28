package iterm2

import (
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
)

type RgbColor struct {
	Red   int
	Green int
	Blue  int
}

const BEL = "\a"
const ESC = "\033"

func PrintOSC() {
	if os.Getenv("TERM") == "screen" {
		fmt.Printf(ESC + `Ptmux;` + ESC + ESC + `]`)
	} else {
		fmt.Printf(ESC + "]")
	}
}

func PrintST() {
	if os.Getenv("TERM") == "screen" {
		fmt.Printf(BEL + ESC + `\`)
	} else {
		fmt.Printf(BEL)
	}
}

func PrintImage(filename string) error {
	options := make(map[string]string)

	filebuf, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	options["name"] = filename
	options["size"] = strconv.Itoa(len(filebuf))
	options["width"] = "auto"
	options["height"] = "auto"
	options["preserveAspectRatio"] = "1"
	options["inline"] = "1"

	PrintOSC()
	fmt.Printf("1337;File=")

	for k, v := range options {
		fmt.Printf("%s=%s;", k, v)
	}

	fmt.Printf(":%s", base64.StdEncoding.EncodeToString(filebuf))

	PrintST()

	return nil
}

func PrintControlSequence(key, val string) {

	PrintOSC()

	b64 := base64.StdEncoding.EncodeToString([]byte(val))
	fmt.Printf("1337;%s=%s", key, b64)

	PrintST()
}

func PrintBadge(msg string) {
	/*
		# Set badge to show the current session name and git branch, if any is set.
		printf "\e]1337;SetBadgeFormat=%s\a" \
		  $(echo -n "\(session.name) \(user.gitBranch)" | base64)
	*/

	PrintControlSequence("SetBadgeFormat", msg)
}

func PrintRemoteHostName(name string) {
	//printf "\e]1337;SetUserVar=%s=%s\a" hostname $(echo -n ${_iterm2_hostname} | base64 -w0)

	PrintControlSequence("SetUserVar=remote_hostname", name)
}

func PrintPath(path string) {
	//printf "\e]1337;SetUserVar=%s=%s\a" path $(echo -n $(pwd) | base64 -w0)
	PrintControlSequence("SetUserVar=path", path)
}

func PrintHostName() {
	hostname, err := os.Hostname()
	if err == nil && hostname != "" {
		PrintControlSequence("SetUserVar=hostname", hostname)
	}
}

func PrintTabTitle(title string) {
	PrintOSC()
	//fmt.Printf("0;%s", title)
	fmt.Printf("0;%s", title)
	PrintST()

}

func PrintTabBGColor(c RgbColor) {
	/*
		echo -e "\033]6;1;bg;red;brightness;255\a"
		echo -e "\033]6;1;bg;green;brightness;0\a"
		echo -e "\033]6;1;bg;blue;brightness;255\a"
	*/

	PrintOSC()
	fmt.Printf("6;1;bg;red;brightness;%d", c.Red)
	PrintST()

	PrintOSC()
	fmt.Printf("6;1;bg;green;brightness;%d", c.Green)
	PrintST()

	PrintOSC()
	fmt.Printf("6;1;bg;blue;brightness;%d", c.Blue)
	PrintST()
}

func PrintResetTabBGColor() {
	PrintOSC()
	// OSC 6 ; 1 ; bg ; * ; default ST
	fmt.Printf("6;1;bg;*;default")
	PrintST()
}
