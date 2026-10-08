package ui

import (
	"path"
	"regexp"
	"strings"
)

// maxTargetWidth caps what describe adds after the process name.
const maxTargetWidth = 24

// interpreters run a script or module named on their command line. For them
// the process name alone ("node", "python3") says little about what is
// actually listening, so the table adds the script.
var interpreters = map[string]bool{
	"node": true, "nodejs": true, "bun": true, "deno": true,
	"ruby": true, "php": true, "perl": true, "java": true, "dotnet": true,
}

var pythonName = regexp.MustCompile(`^python[0-9.]*$`)

// valueFlags take the next argument as their value, so it isn't the script.
var valueFlags = map[string]bool{
	"-c": true, "-e": true, "--eval": true, "-r": true, "--require": true,
	"--import": true, "--loader": true, "-W": true, "-X": true,
	"-cp": true, "-classpath": true, "--class-path": true,
	"-p": true, "--module-path": true,
}

// describe returns what an interpreter process is running, such as "vite"
// for "node node_modules/.bin/vite" or "manage.py" for "python manage.py
// runserver", or "" when name isn't an interpreter or it can't tell.
func describe(name string, args []string) string {
	if !interpreters[name] && !pythonName.MatchString(name) {
		return ""
	}
	if len(args) < 2 {
		return ""
	}
	rest := args[1:]
	var target string
	for i := 0; i < len(rest) && target == ""; i++ {
		a := rest[i]
		switch {
		case a == "-m" && i+1 < len(rest): // python -m http.server
			target = rest[i+1]
		case a == "-jar" && i+1 < len(rest):
			target = path.Base(rest[i+1])
		case valueFlags[a]:
			i++
		case strings.HasPrefix(a, "-"):
		case a == "run" && (name == "deno" || name == "bun"):
		case name == "java": // main class: org.gradle.GradleDaemon -> GradleDaemon
			target = a[strings.LastIndexByte(a, '.')+1:]
		default:
			target = scriptName(a)
		}
	}
	return truncateRight(target, maxTargetWidth)
}

// scriptName shortens a script path to its file name, or to the package name
// for scripts inside node_modules, which are usually CLI tools like vite.
func scriptName(p string) string {
	const nm = "node_modules/"
	i := strings.LastIndex(p, nm)
	if i < 0 {
		return path.Base(p)
	}
	pkg := p[i+len(nm):]
	if bin, ok := strings.CutPrefix(pkg, ".bin/"); ok {
		return bin
	}
	parts := strings.SplitN(pkg, "/", 3)
	if strings.HasPrefix(parts[0], "@") && len(parts) > 1 {
		return parts[0] + "/" + parts[1]
	}
	return parts[0]
}
