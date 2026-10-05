package ast

// Direction represents edge traversal direction.
type Direction int

const (
	DirectionOutgoing   Direction = iota // -[r]->
	DirectionIncoming                    // <-[r]-
	DirectionUndirected                  // -[r]-
)

// NodePattern represents (var:Label {prop: val}).
type NodePattern struct {
	Variable   string
	Labels     []string
	Properties map[string]Expression
}

// RelationshipPattern represents -[r:TYPE*1..3 {prop: val}]->.
type RelationshipPattern struct {
	Variable   string
	Types      []string
	Direction  Direction
	MinHops    *int
	MaxHops    *int
	Properties map[string]Expression
}

// PathElement represents a step: relationship -> target node.
type PathElement struct {
	Relationship RelationshipPattern
	Target       NodePattern
}

// PathPattern represents (head)-[r]->(target)-...
type PathPattern struct {
	Head               NodePattern
	Chain              []PathElement
	PathVariable       string
	IsShortestPath     bool
	IsAllShortestPaths bool
}

// MatchClause represents MATCH or OPTIONAL MATCH.
type MatchClause struct {
	IsOptional bool
	Paths      []PathPattern
	Where      Expression
}

// ProjectionItem represents an item in WITH or RETURN: expr [AS alias].
type ProjectionItem struct {
	Expression Expression
	Alias      string
}

// WithClause represents WITH [DISTINCT] item1, item2 [WHERE ...].
type WithClause struct {
	IsDistinct bool
	Items      []ProjectionItem
	Where      Expression
}

// UnwindClause represents UNWIND expr AS alias.
type UnwindClause struct {
	Expression Expression
	Alias      string
}

// CallClause represents CALL { subquery }.
type CallClause struct {
	Subquery *Query
}

// UnionClause represents UNION [ALL] query.
type UnionClause struct {
	IsAll bool
	Query *Query
}

// ReturnClause represents RETURN [DISTINCT] item1, item2.
type ReturnClause struct {
	IsDistinct bool
	Items      []ProjectionItem
}

// OrderByItem represents expr [ASC|DESC].
type OrderByItem struct {
	Expression Expression
	IsDesc     bool
}

// Query represents a parsed Cypher query.
type Query struct {
	Matches     []MatchClause
	WithClauses []WithClause
	Unwinds     []UnwindClause
	Calls       []CallClause
	Where       Expression
	Return      ReturnClause
	OrderBy     []OrderByItem
	Skip        Expression
	Limit       Expression
	Unions      []UnionClause
}

// Expression is the base interface for AST expressions.
type Expression interface {
	exprNode()
}

// IdentifierExpr represents a named variable (e.g. n, p, r).
type IdentifierExpr struct {
	Name string
}

func (IdentifierExpr) exprNode() {}

// PropertyAccessExpr represents n.property.
type PropertyAccessExpr struct {
	Variable string
	Property string
}

func (PropertyAccessExpr) exprNode() {}

// LiteralKind indicates literal data type.
type LiteralKind int

const (
	LiteralString LiteralKind = iota
	LiteralNumber
	LiteralBool
	LiteralNull
)

// LiteralExpr represents a constant literal value.
type LiteralExpr struct {
	Kind      LiteralKind
	Raw       string
	StrVal    string
	NumVal    float64
	IsInteger bool
	BoolVal   bool
}

func (LiteralExpr) exprNode() {}

// ParameterExpr represents $paramName.
type ParameterExpr struct {
	Name string
}

func (ParameterExpr) exprNode() {}

// BinaryOp represents binary operators.
type BinaryOp string

const (
	OpAdd      BinaryOp = "+"
	OpSub      BinaryOp = "-"
	OpMul      BinaryOp = "*"
	OpDiv      BinaryOp = "/"
	OpMod      BinaryOp = "%"
	OpPower    BinaryOp = "^"
	OpEq       BinaryOp = "="
	OpNeq      BinaryOp = "!="
	OpLt       BinaryOp = "<"
	OpLte      BinaryOp = "<="
	OpGt       BinaryOp = ">"
	OpGte      BinaryOp = ">="
	OpAnd      BinaryOp = "AND"
	OpOr       BinaryOp = "OR"
	OpIn       BinaryOp = "IN"
	OpStarts   BinaryOp = "STARTS WITH"
	OpEnds     BinaryOp = "ENDS WITH"
	OpContains BinaryOp = "CONTAINS"
	OpIs       BinaryOp = "IS"
	OpIsNot    BinaryOp = "IS NOT"
)

// BinaryExpr represents Left OP Right.
type BinaryExpr struct {
	Left  Expression
	Op    BinaryOp
	Right Expression
}

func (BinaryExpr) exprNode() {}

// UnaryOp represents unary operators.
type UnaryOp string

const (
	OpNot   UnaryOp = "NOT"
	OpMinus UnaryOp = "-"
	OpPlus  UnaryOp = "+"
)

// UnaryExpr represents OP Operand.
type UnaryExpr struct {
	Op      UnaryOp
	Operand Expression
}

func (UnaryExpr) exprNode() {}

// FunctionCallExpr represents fn(arg1, arg2).
type FunctionCallExpr struct {
	Name       string
	IsDistinct bool
	Args       []Expression
}

func (FunctionCallExpr) exprNode() {}

// ListExpr represents [item1, item2, ...].
type ListExpr struct {
	Items []Expression
}

func (ListExpr) exprNode() {}

// MapExpr represents {key1: val1, key2: val2}.
type MapExpr struct {
	Entries map[string]Expression
}

func (MapExpr) exprNode() {}

// WildcardExpr represents *.
type WildcardExpr struct{}

func (WildcardExpr) exprNode() {}

// PatternExpr represents (n)-[:REL]->(m) inside WHERE or expressions.
type PatternExpr struct {
	Path PathPattern
}

func (PatternExpr) exprNode() {}

// PatternComprehensionExpr represents [(p)-[:REL]->(m) WHERE ... | m.name].
type PatternComprehensionExpr struct {
	Path       PathPattern
	Filter     Expression
	Projection Expression
}

func (PatternComprehensionExpr) exprNode() {}

// ListPredicateExpr represents any(x IN list WHERE pred), all(), none(), single().
type ListPredicateExpr struct {
	Quantifier string // "any", "all", "none", "single"
	Variable   string
	List       Expression
	Predicate  Expression
}

func (ListPredicateExpr) exprNode() {}

// ListComprehensionExpr represents [x IN list WHERE ... | expr].
type ListComprehensionExpr struct {
	Variable   string
	List       Expression
	Filter     Expression
	Projection Expression
}

func (ListComprehensionExpr) exprNode() {}

// CaseWhen represents WHEN cond THEN result.
type CaseWhen struct {
	When Expression
	Then Expression
}

// CaseExpr represents CASE [test] WHEN ... THEN ... ELSE ... END.
type CaseExpr struct {
	Test         Expression
	WhenBranches []CaseWhen
	Else         Expression
}

func (CaseExpr) exprNode() {}

// HasLabelExpr represents n:Label in WHERE clauses.
type HasLabelExpr struct {
	Node  Expression
	Label string
}

func (HasLabelExpr) exprNode() {}

// MapProjectionElement represents .property or key: expr in a map projection.
type MapProjectionElement struct {
	PropertyName string
	Value        Expression
	IsAllProps   bool
}

// MapProjectionExpr represents base { .prop, key: expr }.
type MapProjectionExpr struct {
	Base     Expression
	Elements []MapProjectionElement
}

func (MapProjectionExpr) exprNode() {}

// ReduceExpr represents reduce(acc = init, x IN list | expr).
type ReduceExpr struct {
	Accumulator string
	Initial     Expression
	Variable    string
	List        Expression
	Expression  Expression
}

func (ReduceExpr) exprNode() {}
