function __FUNCTION__
    set -l tokens (commandline -opc)
    set -l full (commandline -cp)
    set -l partial (commandline -ct)
    for record in (env COMP_WORDS="$full" COMP_CWORD="$partial" __VARIABLE__=fish_complete $tokens[1])
        set -l parts (string split -m 1 , -- $record)
        switch $parts[1]
            case plain
                printf '%s\n' "$parts[2]"
            case file
                __fish_complete_path "$parts[2]"
            case dir
                __fish_complete_directories "$parts[2]"
        end
    end
end
complete -c __PROGRAM__ -f -a '(__FUNCTION__)'
