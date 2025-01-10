package engine

import "fmt"

// DBCmd represents a database command type
type DBCmd int

// QueryMap represents mapping between engine types and commands with their SQL queries
type QueryMap map[Type]map[DBCmd]string

// QueryProvider interface allows each store to provide its own queries
type QueryProvider interface {
	Map() QueryMap
}

// PickQuery returns a query for given store provider, db type and command
func PickQuery(p QueryProvider, dbType Type, cmd DBCmd) (string, error) {
	qmap := p.Map()
	dt, ok := qmap[dbType]
	if !ok {
		return "", fmt.Errorf("unsupported database type %q", dbType)
	}

	query, ok := dt[cmd]
	if !ok {
		return "", fmt.Errorf("unsupported command type %d", cmd)
	}

	return query, nil
}

// AQ adopts placeholders in a query for the database engine
func (e *SQL) AQ(q string) string {
	if e.dbType != Postgres { // no need to replace placeholders for sqlite
		return q
	}

	placeholderCount := 1
	result := ""
	inQuotes := false

	for _, r := range q {
		switch r {
		case '\'':
			inQuotes = !inQuotes
			result += string(r)
		case '?':
			if inQuotes {
				result += string(r)
			} else {
				result += fmt.Sprintf("$%d", placeholderCount)
				placeholderCount++
			}
		default:
			result += string(r)
		}
	}

	return result
}
