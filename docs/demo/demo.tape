# peek's README demo. Record it with docs/demo/record.sh.
Output docs/demo.gif

Set Shell "bash"
Set FontSize 16
Set Width 1100
Set Height 560
Set Padding 24
Set Theme "TokyoNight"
Set TypingSpeed 70ms
Set CursorBlink false

Hide
# peek runs as the same user as the servers: Docker doesn't let even root
# inspect other users' processes without CAP_SYS_PTRACE.
Type "sh /demo/setup.sh && exec setpriv --reuid=dev --regid=dev --init-groups env HOME=/home/dev bash"
Enter
Sleep 1s
Type "cd ~ && clear"
Enter
Sleep 1.5s
Show

# The range leaves out the ports VHS itself uses.
Type "peek -i 3000-8999"
Sleep 400ms
Enter
Sleep 2.5s

# Details for the Next.js server, with its connected clients.
Enter
Sleep 3s
Escape
Sleep 1s

# Sort by uptime (s twice), then back to port order.
Type "s"
Sleep 600ms
Type "s"
Sleep 2s
Type "s"
Sleep 800ms

# Filter, then stop the Django server.
Type "/"
Sleep 300ms
Type "api"
Sleep 1s
Enter
Sleep 1s
Type "x"
Sleep 1.8s
Type "y"
Sleep 2.5s
Escape
Sleep 1.5s

# The key reference.
Type "?"
Sleep 2.5s
Type "q"
Sleep 300ms
Type "q"
Sleep 500ms
