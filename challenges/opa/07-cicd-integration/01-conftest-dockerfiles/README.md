# Conftest + Dockerfiles: Policies in Your Pipeline

## Prerequisites

- OPA CLI installed (`opa version`)
- Conftest installed (`conftest --version`; install with `brew install conftest`)
- Completed section 06 exercises (API authorization)

## Learning Objectives

After completing this exercise, you will be able to:

- Write Conftest policies that validate Dockerfiles against security and best-practice rules
- Distinguish between `deny` rules (hard failures) and `warn` rules (soft warnings)
- Use `conftest test` to evaluate a Dockerfile and interpret the output

## Why This Matters

You can write the most thorough OPA policies in the world, but if they only run when someone remembers to invoke them manually, they add little value. The real payoff comes from running policies automatically in CI/CD pipelines -- blocking bad configurations before they reach production.

Conftest is an open-source tool built on top of OPA's engine, designed specifically for validating configuration files (Dockerfiles, YAML, JSON, HCL) against Rego policies. The difference from using `opa eval` directly is that Conftest knows how to parse each format. For Dockerfiles, it converts them into an array of objects representing each build stage.

The convention is simple: Conftest looks for policies in a `policy/` directory using the `main` package. Inside `main`, `deny` rules generate errors (exit code 1) and `warn` rules generate warnings (exit code 0). That exit code behavior is exactly what CI systems need to gate a pipeline.

### How Conftest Parses a Dockerfile

When Conftest parses a Dockerfile, it produces a structure like this:

```json
{
  "Stages": [
    {
      "Name": "node:latest",
      "Commands": [
        {"Cmd": "from", "Value": ["node:latest"]},
        {"Cmd": "workdir", "Value": ["/app"]},
        {"Cmd": "copy", "Value": [".", "."]},
        {"Cmd": "run", "Value": ["npm install"]},
        {"Cmd": "cmd", "Value": ["node", "server.js"]}
      ]
    }
  ]
}
```

Each Dockerfile instruction becomes an object with `Cmd` (lowercase) and `Value` (an array of strings). Writing policies is a matter of iterating over `input.Stages[_].Commands[_]`.

## Practice

Let's start with a Dockerfile that has several common problems.

Create `Dockerfile`:

```dockerfile
FROM node:latest

WORKDIR /app

ADD package*.json ./

RUN npm install

RUN npm run build

RUN npm test

COPY src/ ./src/

EXPOSE 3000

CMD ["node", "server.js"]
```

This Dockerfile has at least five problems: it uses `latest`, it does not declare a non-root `USER`, it has no `HEALTHCHECK`, it uses `ADD` instead of `COPY`, and it has three separate `RUN` instructions that should be consolidated with `&&`.

Now create the policy directory with our rules.

Create `policy/policy.rego`:

```rego
package main

import rego.v1

# Collect all commands from all stages
commands := [cmd | cmd := input.Stages[_].Commands[_]]

# Extract the names of the commands present
command_names := {cmd.Cmd | cmd := commands[_]}

# --- DENY: do not use :latest tag ---
deny contains msg if {
    some cmd in commands
    cmd.Cmd == "from"
    some val in cmd.Value
    endswith(val, ":latest")
    msg := sprintf("FROM uses ':latest' -- pin the image version for '%s'", [val])
}

# --- DENY: must have a non-root USER ---
deny contains msg if {
    not "user" in command_names
    msg := "No USER instruction -- the container would run as root"
}

# --- WARN: should have HEALTHCHECK ---
warn contains msg if {
    not "healthcheck" in command_names
    msg := "No HEALTHCHECK -- consider adding one for better observability"
}

# --- DENY: use COPY instead of ADD ---
deny contains msg if {
    some cmd in commands
    cmd.Cmd == "add"
    msg := sprintf("Use COPY instead of ADD for '%s' -- ADD has implicit behavior with URLs and tar", [concat(" ", cmd.Value)])
}

# --- WARN: too many separate RUN instructions ---
warn contains msg if {
    run_count := count([cmd | some cmd in commands; cmd.Cmd == "run"])
    run_count > 2
    msg := sprintf("There are %d RUN instructions -- combine them with '&&' to reduce layers", [run_count])
}
```

Run conftest against the Dockerfile:

```bash
conftest test Dockerfile
```

Expected output:

```
FAIL - Dockerfile - main - FROM uses ':latest' -- pin the image version for 'node:latest'
FAIL - Dockerfile - main - No USER instruction -- the container would run as root
FAIL - Dockerfile - main - Use COPY instead of ADD for 'package*.json ./' -- ADD has implicit behavior with URLs and tar
WARN - Dockerfile - main - No HEALTHCHECK -- consider adding one for better observability
WARN - Dockerfile - main - There are 3 RUN instructions -- combine them with '&&' to reduce layers

5 tests, 0 passed, 2 warnings, 3 failures
```

The `FAIL` lines come from `deny` rules, and the `WARN` lines from `warn` rules. Conftest returns exit code 1 when there is at least one `FAIL` -- exactly what you need to break a CI pipeline.

### Intermediate Verification

Let's confirm we can also use `opa eval` as a fallback. Conftest has a command that parses the Dockerfile into JSON:

```bash
conftest parse Dockerfile --parser dockerfile
```

That gives you the JSON structure we saw earlier. You can save it and evaluate directly with OPA:

```bash
conftest parse Dockerfile --parser dockerfile > dockerfile_parsed.json
opa eval --format pretty -i dockerfile_parsed.json -d policy/ "data.main.deny"
```

You should see the same three deny messages as a set.

### Common Mistake: Forgetting the `policy/` Directory Convention

If you put your `.rego` file in the current directory instead of `policy/`, conftest will not find it:

```bash
# This will not work -- conftest looks in policy/ by default
conftest test Dockerfile
# 0 tests, 0 passed, 0 warnings, 0 failures
```

Either move the file to `policy/policy.rego` or use `--policy .` to override the default directory. The convention exists so that every project has a predictable location for its policies.

## Verify What You Learned

**1.** Run conftest and confirm there are exactly 3 failures:

```bash
conftest test Dockerfile 2>&1 | grep "FAIL" | wc -l
```

Expected output:

```
3
```

**2.** Verify the warnings:

```bash
conftest test Dockerfile 2>&1 | grep "WARN" | wc -l
```

Expected output:

```
2
```

**3.** Use `opa eval` as an alternative to verify the deny rules:

```bash
conftest parse Dockerfile --parser dockerfile > dockerfile_parsed.json
opa eval --format pretty -i dockerfile_parsed.json -d policy/ "count(data.main.deny)"
```

Expected output:

```
3
```

## What's Next

You have seen how Conftest validates Dockerfiles. The same approach works for any structured configuration. The next exercise applies Conftest to Kubernetes manifests, catching security issues like missing resource limits, privileged containers, and absent health probes before they reach a cluster.

## Reference

- [Conftest -- official documentation](https://www.conftest.dev/)
- [Conftest Dockerfile parser](https://www.conftest.dev/parser/#dockerfile)
- [Conftest -- deny and warn rules](https://www.conftest.dev/exceptions/)

## Additional Resources

- [Hadolint](https://github.com/hadolint/hadolint) -- a complementary Dockerfile linter to use alongside Conftest
- [Docker Best Practices](https://docs.docker.com/build/building/best-practices/) -- official Docker guidance on writing production Dockerfiles
