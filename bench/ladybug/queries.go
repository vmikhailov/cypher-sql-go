package main

// getBenchmarkQueries returns the standardized 7 query patterns evaluated in the benchmark.
func getBenchmarkQueries() []QuerySpec {
	return []QuerySpec{
		{
			ID:          "Q1",
			Name:        "Exact Point Lookup",
			Description: "Indexed point lookup of single service properties",
			Cypher:      "MATCH (s:Service {name: 'service_420'}) RETURN s.id, s.name, s.layer, s.framework",
		},
		{
			ID:          "Q2",
			Name:        "Filtered Property Scan",
			Description: "Filter 100k nodes by property with LIMIT 50",
			Cypher:      "MATCH (s:Service) WHERE s.layer = 'Application' RETURN s.name, s.framework, s.language LIMIT 50",
		},
		{
			ID:          "Q3",
			Name:        "1-Hop Traversal + Aggregation",
			Description: "Join Service->Database with GROUP BY and ORDER BY",
			Cypher:      "MATCH (s:Service)-[:USES_DB]->(d:Database) RETURN s.name, count(d) AS db_count ORDER BY db_count DESC LIMIT 20",
		},
		{
			ID:          "Q4",
			Name:        "2-Hop Multi-Join Traversal",
			Description: "2-hop join pattern: Service->Service->Database",
			Cypher:      "MATCH (s1:Service)-[:CALLS]->(s2:Service)-[:USES_DB]->(d:Database) RETURN s1.name, s2.name, d.name LIMIT 50",
		},
		{
			ID:          "Q5",
			Name:        "Variable-Length Path (1..3 hops)",
			Description: "Recursive path finding with cycle prevention and DISTINCT",
			Cypher:      "MATCH (s:Service {name: 'service_10'})-[:CALLS*1..3]->(target:Service) RETURN DISTINCT target.name LIMIT 100",
		},
		{
			ID:          "Q6",
			Name:        "Degree Centrality Aggregation",
			Description: "High fan-out relationship scan with GROUP BY and ORDER BY",
			Cypher:      "MATCH (n:Service)-[r:CALLS]->() RETURN n.name, count(r) AS degree ORDER BY degree DESC LIMIT 10",
		},
		{
			ID:          "Q7",
			Name:        "2-Tier Hierarchy (S->C->M)",
			Description: "Targeted 2-hop hierarchy traversal: Service->Class->Method",
			Cypher:      "MATCH (s:Service {name: 'service_50'})-[:CONTAINS]->(c:Class)-[:CONTAINS]->(m:Method) RETURN c.name, count(m) AS method_count ORDER BY method_count DESC LIMIT 10",
		},
	}
}
