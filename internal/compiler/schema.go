package compiler

// SchemaConfig defines physical table and column mappings for Cypher to SQL compilation.
type SchemaConfig struct {
	// Dialect is the database dialect for SQL generation (default: SQLiteDialect()).
	Dialect Dialect
	// NodesTable is the table name for vertices (default: "nodes").
	NodesTable string
	// EdgesTable is the table name for relationships (default: "edges").
	EdgesTable string
	// NodeIDCol is the primary key column for nodes (default: "id").
	NodeIDCol string
	// NodeKindCol is the label/type column for nodes (default: "kind").
	NodeKindCol string
	// NodePropsCol is the JSON properties column for nodes (default: "properties").
	NodePropsCol string
	// EdgeFromCol is the source vertex ID column for edges (default: "from_id").
	EdgeFromCol string
	// EdgeToCol is the target vertex ID column for edges (default: "to_id").
	EdgeToCol string
	// EdgeKindCol is the relationship type column for edges (default: "kind").
	EdgeKindCol string
	// EdgePropsCol is the JSON properties column for edges (default: "properties").
	EdgePropsCol string
	// LabelResolver provides an optional hook to override SQL predicates for node labels.
	LabelResolver func(nodeVar, label string) (string, bool)
}

// DefaultSchemaConfig returns the standard Universal Property Graph schema configuration for SQLite.
func DefaultSchemaConfig() SchemaConfig {
	return SchemaConfig{
		Dialect:      SQLiteDialect(),
		NodesTable:   "nodes",
		EdgesTable:   "edges",
		NodeIDCol:    "id",
		NodeKindCol:  "kind",
		NodePropsCol: "properties",
		EdgeFromCol:  "from_id",
		EdgeToCol:    "to_id",
		EdgeKindCol:  "kind",
		EdgePropsCol: "properties",
	}
}

// ClickHouseSchemaConfig returns a standard SchemaConfig tuned for ClickHouse.
func ClickHouseSchemaConfig() SchemaConfig {
	return SchemaConfig{
		Dialect:      ClickHouseDialect(),
		NodesTable:   "nodes",
		EdgesTable:   "edges",
		NodeIDCol:    "id",
		NodeKindCol:  "kind",
		NodePropsCol: "properties",
		EdgeFromCol:  "from_id",
		EdgeToCol:    "to_id",
		EdgeKindCol:  "kind",
		EdgePropsCol: "properties",
	}
}
