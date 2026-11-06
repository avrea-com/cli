__FUNCTION__() {
    local word quoted words="" record kind value candidate
    COMPREPLY=()
    if type compopt >/dev/null 2>&1; then compopt +o filenames 2>/dev/null; fi
    for word in "${COMP_WORDS[@]}"; do
        printf -v quoted '%q' "$word"
        words="$words$quoted "
    done
    while IFS= read -r record; do
        kind="${record%%,*}"
        value="${record#*,}"
        case "$kind" in
            plain)
                if ! type compopt >/dev/null 2>&1; then value="$value "; fi
                COMPREPLY+=("$value") ;;
            file|dir)
                if type compopt >/dev/null 2>&1; then compopt -o filenames 2>/dev/null; fi
                while IFS= read -r candidate; do
                    if [[ -d "$candidate" ]]; then candidate="$candidate/"; fi
                    # Bash 3.2 has no compopt; quote paths ourselves without
                    # classifying command suggestions as filenames.
                    if ! type compopt >/dev/null 2>&1; then
                        printf -v candidate '%q' "$candidate"
                        if [[ "$candidate" != */ ]]; then candidate="$candidate "; fi
                    fi
                    COMPREPLY+=("$candidate")
                done < <(if [[ "$kind" == dir ]]; then compgen -d -- "$value"; else compgen -f -- "$value"; fi)
                ;;
        esac
    done < <(COMP_WORDS="$words" COMP_CWORD="$COMP_CWORD" __VARIABLE__=bash_complete "${COMP_WORDS[0]}")
}
if type compopt >/dev/null 2>&1; then
    complete -F __FUNCTION__ __PROGRAM__
else
    # Bash 3.2 cannot select filename handling per invocation. Add spaces
    # explicitly for complete words, leaving directories open for more typing.
    complete -o nospace -F __FUNCTION__ __PROGRAM__
fi
