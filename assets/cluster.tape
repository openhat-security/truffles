# truffles cluster — help, status on live fleet, clean-disk dry-run, scale help.
#
#   make demo-vhs TAPE=cluster
#   vhs assets/cluster.tape

Output assets/screenshots/cluster.gif
Output assets/screenshots/cluster.mp4

Require go
Require ffmpeg

Set Shell "bash"
Set FontSize 14
Set Width 1400
Set Height 900
Set TypingSpeed 40ms
Set Theme "Catppuccin Mocha"
Set Padding 16
Set BorderRadius 8
Set WindowBar Colorful
Set Framerate 30

Hide
Type "unset NO_COLOR FORCE_COLOR; export TERM=xterm-256color CLICOLOR=1"
Enter
Sleep 300ms
Type "test -x ./truffles || go build -o truffles ./cmd/truffles"
Enter
Sleep 2s
Type "test -f remote.env || { echo 'missing remote.env'; exit 1; }"
Enter
Sleep 800ms
Type "clear"
Enter
Sleep 500ms
Show

Type "./truffles cluster -h"
Sleep 1.8s
Enter
Sleep 10s

Type "./truffles cluster status -c remote-playbook.yaml"
Sleep 2s
Enter
Sleep 8s

Type "./truffles cluster clean-disk -c remote-playbook.yaml --dry-run"
Sleep 2s
Enter
Sleep 12s

Type "./truffles cluster scale -h"
Sleep 1.5s
Enter
Sleep 7s

Type "echo '✓ cluster status / clean-disk / scale — fleet ready'"
Sleep 1.8s
Enter
Sleep 4s
