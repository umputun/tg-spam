package engine

import "fmt"

// DBCmd represents a database command type
type DBCmd int

// QueryMap represents mapping between engine types and commands with their SQL queries
type QueryMap map[Type]map[DBCmd]string

// PickQuery returns a query for given QueryMap, db type and command
func PickQuery(qmap QueryMap, dbType Type, cmd DBCmd) (string, error) {
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
