Agent instruction file for AI tooling. This is not a product or project document.

AI startup flow:
1. Run `aipmc start`
2. Run `aipmc search "<topic>"`
3. If related work exists, use `aipmc task show`, `aipmc plan show`, `aipmc decision show`, or `aipmc task note` first
4. Only use `add` when existing context clearly does not fit
5. At the end, run `aipmc session close --from-commits --from-tasks`
