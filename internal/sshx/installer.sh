# shellcheck shell=sh
install_homebox() (
    umask 077
    case ${HOME:-} in
        /*) ;;
        *) printf '%s\n' 'HOME must be an absolute path' >&2; exit 1 ;;
    esac
    ssh_dir="$HOME/.ssh"
    include='Include ~/.ssh/homebox_config'
    mkdir -p "$ssh_dir"

    ssh_dir=$(CDPATH='' cd -P "$ssh_dir" && pwd -P)
    config="$ssh_dir/config"

    # Follow dotfile symlinks without replacing the link itself.
    hops=0
    while [ -L "$config" ]; do
        hops=$((hops + 1))
        [ "$hops" -le 40 ] || { printf '%s\n' 'Too many SSH config symlinks' >&2; exit 1; }
        target=$(readlink "$config")
        case $target in
            /*) config=$target ;;
            *) config=${config%/*}/$target ;;
        esac
    done
    if [ "$hops" -gt 0 ] && [ ! -f "$config" ]; then
        printf '%s\n' 'SSH config symlink must point to an existing regular file' >&2
        exit 1
    fi
    config_dir=$(CDPATH='' cd -P "${config%/*}" && pwd -P)
    config="$config_dir/${config##*/}"
    case $config in
        "$ssh_dir/homebox_config"|"$ssh_dir/homebox_known_hosts")
            printf '%s\n' 'SSH config points to a homebox-managed file' >&2
            exit 1 ;;
    esac
    for path in "$config" "$ssh_dir/homebox_config" "$ssh_dir/homebox_known_hosts"; do
        if [ -L "$path" ] || { [ -e "$path" ] && [ ! -f "$path" ]; }; then
            printf 'Expected a regular file at %s\n' "$path" >&2
            exit 1
        fi
    done

    stage=$(mktemp -d "$ssh_dir/.homebox.XXXXXX")
    new_config=
    trap 'rm -rf "$stage"; if [ -n "$new_config" ]; then rm -f "$new_config"; fi' 0
    trap 'exit 1' HUP INT TERM
    write_payload
    if [ -f "$config" ]; then
        IFS= read -r first_line < "$config" || :
    else
        first_line=
    fi
    if [ "$first_line" != "$include" ]; then
        new_config=$(mktemp "${config%/*}/.homebox-config.XXXXXX")
        printf '%s\n' "$include" > "$new_config"
        if [ -f "$config" ]; then
            cat "$config" >> "$new_config"
            backup=$(mktemp "$ssh_dir/config.homebox-backup.XXXXXX")
            cat "$config" > "$backup"
        fi
    fi

    mv -f "$stage/known_hosts" "$ssh_dir/homebox_known_hosts"
    mv -f "$stage/config" "$ssh_dir/homebox_config"
    if [ -n "$new_config" ]; then
        mv -f "$new_config" "$config"
    fi
)

install_homebox
