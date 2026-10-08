# Security Policy

## Supported versions

`peek` is pre-1.0. Security fixes go into the latest version on `main`.

## Reporting a vulnerability

Please **don't** open a public issue for security problems.

Instead, report it privately through GitHub: open the [Security tab](https://github.com/skinleak/peek/security) of this repository and click **Report a vulnerability**. Include:

- a description of the problem and its impact
- steps to reproduce it
- the affected version or commit

You'll get a response as soon as possible. Once the issue is confirmed, a fix will be prepared and you'll be credited in the release notes unless you'd rather stay anonymous.

## Scope

`peek` reads process information from `/proc` and can send signals to processes. Issues that are especially relevant:

- signalling a process other than the one shown to the user
- exposing information about other users' processes that the OS would not normally reveal
- crashes or hangs triggered by unusual `/proc` contents
