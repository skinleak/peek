package main

import (
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/skinleak/peek/internal/scan"
	"github.com/skinleak/peek/internal/ui"
)

// completionScripts are printed by `peek completion <shell>`. They complete
// subcommands, flags, directories (for words starting with ., / or ~) and the
// ports that are listening right now, which they get from `peek __ports`.
var completionScripts = map[string]string{
	"bash": bashCompletion,
	"zsh":  zshCompletion,
	"fish": fishCompletion,
}

func runCompletion(shell string) int {
	fmt.Print(completionScripts[shell])
	return exitOK
}

// runListPorts prints each listening port and what holds it, separated by a
// tab, for the completion scripts. It never fails loudly: a completion that
// prints errors would garble the user's command line.
func runListPorts() int {
	ls, _, err := listen(nil, nil)
	if err != nil {
		return exitNotFound
	}
	writePorts(os.Stdout, ls)
	return exitOK
}

// writePorts writes one "port<TAB>label" line per port, in port order. A port
// with several owners is labelled with the first.
func writePorts(w io.Writer, ls []scan.Listener) {
	ls = slices.Clone(ls)
	slices.SortStableFunc(ls, func(a, b scan.Listener) int { return int(a.Port) - int(b.Port) })
	var last uint16
	for _, l := range ls {
		if l.Port == last {
			continue
		}
		last = l.Port
		fmt.Fprintf(w, "%d\t%s\n", l.Port, ui.ProcessLabel(l))
	}
}

const bashCompletion = `# bash completion for peek. Load it with:
#   source <(peek completion bash)
# or save it to a file in ~/.local/share/bash-completion/completions/peek.

_peek() {
    local cur=${COMP_WORDS[COMP_CWORD]} prev=${COMP_WORDS[COMP_CWORD-1]}
    local sub=${COMP_WORDS[1]}

    case $prev in
        completion) COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur")); return ;;
        -t|--timeout) return ;;
    esac

    if [[ $cur == -* ]]; then
        local flags="--json -i --interactive -h --help --version"
        case $sub in
            kill) flags="-y --yes -f --force -h --help" ;;
            wait) flags="--free -t --timeout -h --help" ;;
        esac
        COMPREPLY=($(compgen -W "$flags" -- "$cur"))
        return
    fi

    if [[ $cur == [./~]* || $cur == */* ]]; then
        [[ $sub == wait ]] && return
        compopt -o filenames 2>/dev/null
        COMPREPLY=($(compgen -d -- "$cur"))
        return
    fi

    local words
    words=$(command peek __ports 2>/dev/null | cut -f1)
    if [[ $COMP_CWORD == 1 ]]; then
        words="kill wait completion $words"
    fi
    COMPREPLY=($(compgen -W "$words" -- "$cur"))
}

complete -F _peek peek
`

const zshCompletion = `#compdef peek
# zsh completion for peek. Load it with:
#   source <(peek completion zsh)
# or save it as _peek in a directory on your $fpath.

_peek() {
    local sub=${words[2]}
    local -a flags subcommands ports

    case ${words[CURRENT-1]} in
        completion) compadd bash zsh fish; return ;;
        -t|--timeout) _message 'duration, e.g. 30s or 2m'; return ;;
    esac

    if [[ $PREFIX == -* ]]; then
        case $sub in
            kill) flags=("-y:don't ask for confirmation" "--yes:don't ask for confirmation"
                         '-f:send SIGKILL instead of SIGTERM' '--force:send SIGKILL instead of SIGTERM') ;;
            wait) flags=('--free:wait until the ports are free instead'
                         '-t:give up after a duration' '--timeout:give up after a duration') ;;
            *) flags=('--json:machine-readable output' '-i:live view' '--interactive:live view'
                      '--version:print the version') ;;
        esac
        flags+=('-h:show help' '--help:show help')
        _describe flag flags
        return
    fi

    if [[ $PREFIX == [./~]* || $PREFIX == */* ]]; then
        [[ $sub == wait ]] || _files -/
        return
    fi

    if (( CURRENT == 2 )); then
        subcommands=('kill:stop the processes on ports or from a project'
                     'wait:wait until ports are listening, or free'
                     'completion:print a shell completion script')
        _describe -V command subcommands
    fi
    ports=(${(f)"$(command peek __ports 2>/dev/null)"})
    ports=(${ports//$'\t'/:})
    _describe -V port ports
}

if [[ $zsh_eval_context[-1] == loadautofunc ]]; then
    _peek "$@"
else
    compdef _peek peek
fi
`

const fishCompletion = `# fish completion for peek. Load it with:
#   peek completion fish | source
# or save it to ~/.config/fish/completions/peek.fish.

function __peek_dir_token
    string match -qr '^[./~]|/' -- (commandline -ct)
end

complete -c peek -f

complete -c peek -n __fish_use_subcommand -a kill -d 'Stop the processes on ports or from a project'
complete -c peek -n __fish_use_subcommand -a wait -d 'Wait until ports are listening, or free'
complete -c peek -n __fish_use_subcommand -a completion -d 'Print a shell completion script'
complete -c peek -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'

complete -c peek -n 'not __fish_seen_subcommand_from completion; and not __peek_dir_token' -a '(command peek __ports 2>/dev/null)'
complete -c peek -n 'not __fish_seen_subcommand_from completion wait; and __peek_dir_token' -a '(__fish_complete_directories (commandline -ct))'

complete -c peek -n 'not __fish_seen_subcommand_from kill wait completion' -l json -d 'Machine-readable output'
complete -c peek -n 'not __fish_seen_subcommand_from kill wait completion' -s i -l interactive -d 'Live view'
complete -c peek -n '__fish_seen_subcommand_from kill' -s y -l yes -d "Don't ask for confirmation"
complete -c peek -n '__fish_seen_subcommand_from kill' -s f -l force -d 'Send SIGKILL instead of SIGTERM'
complete -c peek -n '__fish_seen_subcommand_from wait' -l free -d 'Wait until the ports are free instead'
complete -c peek -n '__fish_seen_subcommand_from wait' -s t -l timeout -x -d 'Give up after a duration, e.g. 30s'
complete -c peek -s h -l help -d 'Show help'
complete -c peek -n __fish_use_subcommand -l version -d 'Print the version'
`
