#compdef __PROGRAM__
__FUNCTION__() {
    local kind value description
    local -a described plain
    while IFS= read -r kind && IFS= read -r value && IFS= read -r description; do
        case "$kind" in
            plain)
                if [[ "$description" == _ ]]; then
                    plain+=("$value")
                else
                    described+=("$value:$description")
                fi
                ;;
            file) _files ;;
            dir) _files -/ ;;
        esac
    done < <(COMP_WORDS="${(j: :)${(@q)words}}" COMP_CWORD="$((CURRENT-1))" __VARIABLE__=zsh_complete "$words[1]")
    (( ${#plain} )) && compadd -- "${plain[@]}"
    (( ${#described} )) && _describe 'avr' described
    return 0
}
compdef __FUNCTION__ __PROGRAM__
case "${zsh_eval_context[-1]}" in
    loadautofunc) __FUNCTION__ "$@" ;;
esac
