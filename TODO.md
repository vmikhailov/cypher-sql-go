# Security & Compiler Audit: Comprehensive TODO List

This document outlines security vulnerabilities, functional compiler defects, dialect compatibility gaps, and MCP server integrity improvements identified during the architecture and security audit of `cypher-sql-go` and `cypher-mcp`.

---

## 1. Security & Injection Vulnerabilities

### 1.1 Identifier SQL Injection
- **Location:** `internal/compiler/compiler.go:372` (`kind = '` + label + `'`), `internal/compiler/compiler_expr.go:84, 89`, and `internal/compiler/compiler.go:360` (`'$.' + k`).
- **Description:** Label names, relationship types, and property keys enclosed in backticks in Cypher were passed unescaped directly into SQL string literals or JSON paths.
- **PoC:**
  ```cypher
  MATCH (p:`Person'; DELETE FROM nodes; --`) RETURN count(p)
  MATCH (p:`Person' UNION SELECT name FROM sqlite_master --`) RETURN p.name
  ```
- **Remediation:** Validate all identifiers (labels, rel types, property names, aliases, and variables) against `^[a-zA-Z_][a-zA-Z0-9_]*$` using `ValidateIdentifier()`. Reject invalid characters during parsing or compilation.

### 1.2 Map Literal Key SQL Injection
- **Location:** `internal/compiler/compiler_expr.go:387` (`visitMap`).
- **Description:** In `visitMap`, keys are appended directly inside single quotes: `pairs = append(pairs, "'"+k+"', "+vSQL)`. If a key contains single quotes (e.g. from an escaped identifier), it breaks out of the SQL string literal.
- **Remediation:** Escape single quotes via `SanitizeSQLLiteral(k)` and validate identifier formatting.

### 1.3 Function Name Injection in `visitFunctionCall`
- **Location:** `internal/compiler/compiler_expr.go:365`.
- **Description:** `return fn.Name + "(" + distinctStr + strings.Join(argStrings, ", ") + ")", nil`. If an arbitrary function name is enclosed in backticks (e.g. `` `sqlite_version() --`() ``), it is injected raw into the generated SQL `SELECT` expression.
- **Remediation:** Enforce identifier validation on `fn.Name` or restrict function calls to an allowlist of known/safe SQL and Cypher scalar functions.

### 1.4 Alias & Variable Quoting Escapes
- **Location:** `internal/compiler/compiler.go:493` (`cols = append(cols, exprSQL+` AS "`+alias+`"`)`) and `internal/compiler/compiler.go:93-98` (`escapeVar`).
- **Description:** Double-quoted aliases (`AS "alias"`) do not escape inner double quotes (`"`). Furthermore, `escapeVar` only wraps keywords in double quotes and returns variables with special characters raw.
- **Remediation:** Validate aliases/variables against safe identifier rules and escape double quotes using standard SQL escaping (`""`).

### 1.5 Multi-Statement Execution Defense
- **Location:** `internal/parser/lexer.go` / `cypher-mcp/main.go`.
- **Description:** Go SQLite drivers (e.g. `modernc.org/sqlite`) can execute multiple statements separated by `;`.
- **Remediation:** Disallow unquoted semicolons in Cypher queries, and ensure `cypher-mcp` rejects multi-statement strings before execution.

### 1.6 Read-Only Enclosure in MCP Server
- **Location:** `cypher-mcp/main.go:178-191` (`handleGraphQuery`).
- **Description:** `handleGraphQuery` runs on the shared read-write `*sql.DB` connection.
- **Remediation:** Open a dedicated connection with `mode=ro` (or execute `PRAGMA query_only = ON;`) for `graph_query` calls, providing defense-in-depth even if an injection vector bypasses the compiler.

---

## 2. Critical Compiler Functional Bugs

### 2.1 Node Pattern Property Lookup on Root Columns (`{id: ...}`)
- **Location:** `internal/compiler/compiler.go:355-364` (`addNodeFiltersToConditions`).
- **Description:** A query such as `MATCH (p:Person {id: 'person:slava'}) RETURN p` compiles to:
  ```sql
  json_extract(p.properties, '$.id') = 'person:slava'
  ```
  However, in the SQLite physical schema, `id` and `kind` reside in root table columns (`nodes.id`, `nodes.kind`), NOT inside the JSON `properties` column. Meanwhile, property access `p.id` in `visitPropertyAccess` maps correctly to `p.id`. Consequently, pattern property matching on `id` returns 0 rows.
- **Remediation:** Check property keys in `addNodeFiltersToConditions`: if key matches `c.schema.NodeIDCol` (`id`) or `c.schema.NodeKindCol` (`kind`), emit `nVar.id = valSQL` or `nVar.kind = valSQL`.

### 2.2 Relationship Properties (`rel.Properties`) Silently Ignored
- **Location:** `internal/compiler/compiler.go:270-348` (`processPathChain`).
- **Description:** The parser successfully populates `rel.Properties` (e.g. for `-[r:CALLS {status: 'active'}]->`), but `processPathChain` never inspects `rel.Properties`. Filter predicates on relationships are silently dropped without error.
- **Remediation:** Iterate over `rel.Properties` in `processPathChain` and append matching conditions (`json_extract(r.properties, '$.k') = val`) to `relOnConds`.

### 2.3 Dropped Head Node from JOINs in Multi-Pattern Traversal (Target-Bound Bug)
- **Location:** `internal/compiler/compiler.go:207-221` (`processPathPattern`).
- **Description:** When multiple patterns exist (e.g. `MATCH (a:Person)-[:WORKS_AT]->(b:Company) MATCH (c:Person)-[:MANAGES]->(b) RETURN a, b, c`), the compiler sees that target node `b` is already declared (`anyDeclared = true`) and skips calling `bindHeadNode` for `c`. In `processPathChain`, it emits `JOIN edges r ON r.from_id = c.id`, but `nodes c` is never joined to the query.
- **Failure:** SQLite runtime error: `no such column: c.id`.
- **Remediation:** Ensure any unjoined node in the path (including `headVar`) is added to the `JOIN` structure.

### 2.4 Standalone `RETURN` without `MATCH` Generates Broken SQL
- **Location:** `internal/compiler/templates.go:77` (`queryTemplate`).
- **Description:** Constant queries like `RETURN 1 + 1 AS res` or `RETURN date()` leave `c.fromTable` empty. The template renders:
  ```sql
  SELECT (1 + 1) AS "res" FROM
  ```
- **Remediation:** Make the `FROM` clause conditional in `queryTemplate`: `{{- if .FromTable }}FROM {{ .FromTable }} {{ .FromAlias }}{{ end }}`.

### 2.5 Unimplemented `id(n)` and `elementId(n)` Functions
- **Location:** `internal/compiler/compiler_expr.go:214-366` (`visitFunctionCall`).
- **Description:** OpenCypher standard functions `id(n)` and `elementId(n)` fall through to SQLite function calls `id(n)` / `elementId(n)`, which do not exist in SQLite.
- **Remediation:** Map `id(n)` and `elementId(n)` directly to `nVar.id` in `visitFunctionCall`.

### 2.6 Variable-Length Paths (`-[*1..3]->`) Silently Emit Single Hops
- **Location:** `internal/parser/parser.go:429` / `internal/compiler/compiler.go`.
- **Description:** `MinHops` and `MaxHops` are read by the parser, but never consumed by the compiler. A variable-length path compiles into a single JOIN, producing incorrect empty results instead of traversing multiple hops.
- **Remediation:** Either implement recursive CTE (`WITH RECURSIVE`) path traversal or return an explicit compilation error: `variable-length paths are not yet supported`.

### 2.7 Wildcard Inaccuracy in `CONTAINS`, `STARTS WITH`, `ENDS WITH`
- **Location:** `internal/compiler/compiler_expr.go:183-187`.
- **Description:** Translated to SQL `LIKE ('%' || val || '%')`. Unlike Cypher (which performs literal substring matches), SQL `LIKE` treats `%` and `_` inside `val` as wildcards. Searching for `'100%'` or `'file_name'` matches unintended patterns.
- **Remediation:** Escape `%`, `_`, and escape characters in literal arguments and append `ESCAPE '\'`.

---

## 3. Dialect Compatibility (SQLite vs ClickHouse)

### 3.1 Hardcoded `json_each` in Binary `IN` Operator
- **Location:** `internal/compiler/compiler_expr.go:181`.
- **Description:** The `IN` operator compiles to `val IN (SELECT value FROM json_each(list))`. ClickHouse lacks `json_each` and will fail with a syntax error.
- **Remediation:** Delegate `IN` operator SQL rendering to `Dialect` (e.g. `has(list, val)` in ClickHouse).

## 4. Actionable Roadmap & Checklist

### 🛡️ Security
- [x] 1. Enforce strict identifier validation (`^[a-zA-Z_][a-zA-Z0-9_]*$`) for labels, relationship types, properties, variables, and aliases.
- [x] 2. Sanitize and escape single quotes for map literal keys in `visitMap`.
- [x] 3. Validate function names in `visitFunctionCall` against safe identifier regex.
- [x] 4. Reject queries containing unquoted semicolons (prevent multi-statement execution).

### 🐛 Compiler Core
- [x] 5. Fix `{id: ...}` and `{kind: ...}` in node patterns by checking root schema columns (`nodes.id`, `nodes.kind`).
- [x] 6. Add relationship property filter handling (`rel.Properties`) in `processPathChain`.
- [x] 7. Resolve target-bound multi-pattern JOIN bug where `headVar` is omitted from `FROM`/`JOIN`.
- [x] 8. Make `FROM` clause conditional in `queryTemplate` to support standalone `RETURN` expressions.
- [x] 9. Implement recursive CTE path expansion for `-[*1..3]->` or return an explicit, descriptive error.
- [x] 10. Add built-in translation for `id(n)` and `elementId(n)` to `n.id`.
- [x] 11. Escape `%` and `_` wildcards in `STARTS WITH`, `ENDS WITH`, `CONTAINS` using `ESCAPE '\'`.
- [x] 12. Abstract `IN` expression rendering through `Dialect` to support ClickHouse.

