# truffles — author a playbook in vim, then run it against openhat-security.
#
#   make demo-vhs TAPE=playbook
#   vhs assets/playbook.tape

Output assets/screenshots/playbook.gif
Output assets/screenshots/playbook.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 15
Set Width 1280
Set Height 800
Set TypingSpeed 32ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

Hide
Type "unset NO_COLOR FORCE_COLOR; export TERM=xterm-256color CLICOLOR=1"
Enter
Sleep 200ms
Type "test -x ./truffles || go build -o truffles ./cmd/truffles"
Enter
Sleep 2s
Type "rm -f demo-playbook.yaml repos-demo.txt"
Enter
Sleep 300ms
Type "clear"
Enter
Sleep 400ms
Show

Type "vim demo-playbook.yaml"
Enter
Sleep 1.2s

Type "i"
Sleep 400ms
Type "name: openhat-run"
Enter
Type "description: search openhat-security for *run*, then scan"
Enter
Type "search:"
Enter
Type "  owner: openhat-security"
Enter
Type "  queries:"
Enter
Type '    - "*run*"'
Enter
Type "  out: repos-demo.txt"
Enter
Type "  no_proxy: true"
Enter
Type "  workers: 4"
Enter
Type "scan:"
Enter
Type "  file: repos-demo.txt"
Enter
Type "  workers: 2"
Enter
Type "  format: pretty"
Enter
Type "  exclude_paths:"
Enter
Type "    - node_modules"
Enter
Type "    - vendor"
Enter
Type '    - "*.lock"'
Enter
Type "log_dir: /tmp/truffles-demo-logs"
Escape
Sleep 800ms
Type ":wq"
Enter
Sleep 1s

Type "cat demo-playbook.yaml"
Enter
Sleep 2s

Type "./truffles playbook -f demo-playbook.yaml"
Enter
Sleep 18s

Type "echo '✓ playbook search → scan complete'"
Enter
Sleep 2s
