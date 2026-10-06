package main

// getBenchmarkQueries returns the standardized 10 query patterns partitioned into OLTP and OLAP suites.
func getBenchmarkQueries() []QuerySpec {
	return []QuerySpec{
		// =========================================================================
		// SUITE A: TRANSACTIONAL & LOCALIZED GRAPH WORKLOAD (OLTP)
		// =========================================================================
		{
			ID:          "Q1",
			Category:    "OLTP",
			Name:        "Exact Point Lookup",
			Description: "Indexed point lookup of single service properties by primary key",
			Cypher:      "MATCH (s:Service {name: 'service_420'}) RETURN s.id, s.name, s.layer, s.framework",
			DuckPGQ: `FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service WHERE s.name = 'service_420')
  COLUMNS (s.id, s.name, s.layer, s.framework)
);`,
		},
		{
			ID:          "Q2",
			Category:    "OLTP",
			Name:        "Filtered Property Scan (Limit 50)",
			Description: "Filter 100k nodes by property with early-exit LIMIT 50",
			Cypher:      "MATCH (s:Service) WHERE s.layer = 'Application' RETURN s.name, s.framework, s.language LIMIT 50",
			DuckPGQ: `FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service WHERE s.layer = 'Application')
  COLUMNS (s.name, s.framework, s.language)
) LIMIT 50;`,
		},
		{
			ID:          "Q3",
			Category:    "OLTP",
			Name:        "Localized 1-Hop Traversal (Limit 20)",
			Description: "Service->Database traversal with GROUP BY and LIMIT 20",
			Cypher:      "MATCH (s:Service)-[:USES_DB]->(d:Database) RETURN s.name, count(d) AS db_count ORDER BY db_count DESC LIMIT 20",
			DuckPGQ: `SELECT s_name, count(d_name) AS db_count
FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service)-[r:USES_DB]->(d:Database)
  COLUMNS (s.name AS s_name, d.name AS d_name)
)
GROUP BY s_name
ORDER BY db_count DESC
LIMIT 20;`,
		},
		{
			ID:          "Q4",
			Category:    "OLTP",
			Name:        "Localized 2-Hop Traversal (Limit 50)",
			Description: "2-hop join pattern: Service->Service->Database with LIMIT 50",
			Cypher:      "MATCH (s1:Service)-[:CALLS]->(s2:Service)-[:USES_DB]->(d:Database) RETURN s1.name, s2.name, d.name LIMIT 50",
			DuckPGQ: `FROM GRAPH_TABLE (
  microservices
  MATCH (s1:Service)-[c:CALLS_SERVICE]->(s2:Service)-[u:USES_DB]->(d:Database)
  COLUMNS (s1.name AS s1_name, s2.name AS s2_name, d.name AS d_name)
) LIMIT 50;`,
		},
		{
			ID:          "Q5",
			Category:    "OLTP",
			Name:        "Localized Hierarchy Traversal (Limit 10)",
			Description: "Targeted 2-hop hierarchy traversal: Service->Class->Method with LIMIT 10",
			Cypher:      "MATCH (s:Service {name: 'service_50'})-[:CONTAINS]->(c:Class)-[:CONTAINS]->(m:Method) RETURN c.name, count(m) AS method_count ORDER BY method_count DESC LIMIT 10",
			DuckPGQ: `SELECT c_name, count(m_name) AS method_count
FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service WHERE s.name = 'service_50')-[r1:CONTAINS_SC]->(c:Class)-[r2:CONTAINS_CM]->(m:Method)
  COLUMNS (c.name AS c_name, m.name AS m_name)
)
GROUP BY c_name
ORDER BY method_count DESC
LIMIT 10;`,
		},

		// =========================================================================
		// SUITE B: STRUCTURAL & ANALYTICAL GRAPH WORKLOAD (OLAP)
		// =========================================================================
		{
			ID:          "Q6",
			Category:    "OLAP",
			Name:        "Unconstrained 2-Hop Full Join",
			Description: "Global unconstrained multi-hop join: Service->Service->Database counting all 15k paths",
			Cypher:      "MATCH (s1:Service)-[:CALLS]->(s2:Service)-[:USES_DB]->(d:Database) RETURN count(*) AS total_paths",
			DuckPGQ: `SELECT count(*) FROM GRAPH_TABLE (
  microservices
  MATCH (s1:Service)-[c:CALLS_SERVICE]->(s2:Service)-[u:USES_DB]->(d:Database)
  COLUMNS (s1.id AS s1_id)
);`,
		},
		{
			ID:          "Q7",
			Category:    "OLAP",
			Name:        "Deep Path Expansion (k=1..5)",
			Description: "Recursive path finding with cycle prevention and reachable distinct target count",
			Cypher:      "MATCH (s:Service {name: 'service_10'})-[:CALLS*1..5]->(target:Service) RETURN count(DISTINCT target.name) AS reachable_count",
			DuckPGQ: `SELECT count(DISTINCT target_name) FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service WHERE s.name = 'service_10')-[c:CALLS_SERVICE]->{1,5}(target:Service)
  COLUMNS (target.name AS target_name)
);`,
		},
		{
			ID:          "Q8",
			Category:    "OLAP",
			Name:        "Global Property Filter Aggregation",
			Description: "Full table scan across 100k nodes filtering by unindexed property with global COUNT",
			Cypher:      "MATCH (s:Service) WHERE s.framework = 'express' RETURN count(s) AS express_count",
			DuckPGQ:     `SELECT count(*) FROM Service WHERE framework = 'express';`,
		},
		{
			ID:          "Q9",
			Category:    "OLAP",
			Name:        "Global Topology Edge Aggregation",
			Description: "Global relationship scan evaluating raw edge table traversal without anchor shortcuts",
			Cypher:      "MATCH (a:Service)-[r:CALLS]->(b:Service) RETURN count(r) AS total_calls",
			DuckPGQ: `SELECT count(*) FROM GRAPH_TABLE (
  microservices
  MATCH (s1:Service)-[c:CALLS_SERVICE]->(s2:Service)
  COLUMNS (s1.id AS s1_id)
);`,
		},
		{
			ID:          "Q10",
			Category:    "OLAP",
			Name:        "High Fan-Out Degree Centrality",
			Description: "High fan-out relationship scan with global GROUP BY and ORDER BY",
			Cypher:      "MATCH (n:Service)-[r:CALLS]->() RETURN n.name, count(r) AS degree ORDER BY degree DESC LIMIT 10",
			DuckPGQ: `SELECT s_name, count(*) AS degree
FROM GRAPH_TABLE (
  microservices
  MATCH (s:Service)-[c:CALLS_SERVICE]->(target:Service)
  COLUMNS (s.name AS s_name)
)
GROUP BY s_name
ORDER BY degree DESC
LIMIT 10;`,
		},
	}
}
