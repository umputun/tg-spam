package engine

import "fmt"

// DBCmd represents a database command type
type DBCmd int

// Query represents a SQL query with dialect-specific variants
type Query struct {
	Sqlite   string
	Postgres string
}

// QueryMap represents mapping between commands and their SQL queries
type QueryMap struct {
	queries map[DBCmd]Query
}

// NewQueryMap creates a new QueryMap
func NewQueryMap() *QueryMap {
	return &QueryMap{queries: make(map[DBCmd]Query)}
}

// Add adds queries for a command with dialect-specific versions
func (q *QueryMap) Add(cmd DBCmd, query Query) *QueryMap {
	q.queries[cmd] = query
	return q
}

// AddSame adds the same query for all dialects
func (q *QueryMap) AddSame(cmd DBCmd, query string) *QueryMap {
	return q.Add(cmd, Query{Sqlite: query, Postgres: query})
}

// Pick returns a query for given db type and command
func (q *QueryMap) Pick(dbType Type, cmd DBCmd) (string, error) {
	query, ok := q.queries[cmd]
	if !ok {
		return "", fmt.Errorf("unsupported command type %d", cmd)
	}

	switch dbType {
	case Sqlite:
		return query.Sqlite, nil
	case Postgres:
		return query.Postgres, nil
	default:
		return "", fmt.Errorf("unsupported database type %q", dbType)
	}
}
