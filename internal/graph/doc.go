// Package graph is the graph view (spec §7): works joined to their
// authors, series, subjects and narrators, and to the works nearest them in
// the content view, with books ranked from seeds by personalized PageRank.
//
// Embeddings and structure are complementary. The content view finds books
// that read like a seed; the graph follows what the catalog says about
// them (same author, same series, same shelf) and can take several steps,
// so it reaches books no seed is directly like.
//
// BuildNeighbours rebuilds text_neighbour, each text's nearest texts, which
// is slow and changes only when texts are embedded; Load reads the graph
// from the derived tables (edges.sql) into memory, and Rank walks it.
package graph
