package https

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	pathpkg "path"
	"strings"
	"time"

	"dfsha/common"
)

func registerFilesystemRoutes(mux *http.ServeMux, database *sql.DB) {
	mux.HandleFunc("GET "+common.DirectoriesPath, func(w http.ResponseWriter, r *http.Request) {
		listDirectory(w, r, database)
	})
	mux.HandleFunc("POST "+common.DirectoriesPath, func(w http.ResponseWriter, r *http.Request) {
		createDirectory(w, r, database)
	})
	mux.HandleFunc("DELETE "+common.DirectoriesPath, func(w http.ResponseWriter, r *http.Request) {
		removeDirectory(w, r, database)
	})
}

func authenticatedUserID(r *http.Request, database *sql.DB) (int64, error) {
	fields := strings.Fields(r.Header.Get("Authorization"))
	if len(fields) != 2 || !strings.EqualFold(fields[0], "Bearer") {
		return 0, errors.New("missing bearer token")
	}

	hash := sha256.Sum256([]byte(fields[1]))
	tokenHash := hex.EncodeToString(hash[:])

	var userID int64
	err := database.QueryRow(
		`SELECT user_id FROM sessions WHERE token_hash = ? AND expires_at > ?`,
		tokenHash,
		time.Now().Unix(),
	).Scan(&userID)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, errors.New("invalid or expired session")
		}
		return 0, err
	}

	return userID, nil
}

func cleanRemotePath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "/", nil
	}

	raw = strings.ReplaceAll(raw, "\\", "/")

	for _, part := range strings.Split(raw, "/") {
		if part == ".." {
			return "", errors.New("parent-directory traversal is not allowed")
		}
	}

	cleaned := pathpkg.Clean("/" + raw)
	if cleaned == "." {
		return "/", nil
	}

	return cleaned, nil
}

func ensureRoot(database *sql.DB, userID int64) error {
	_, err := database.Exec(
		`INSERT INTO entries (user_id, parent_id, name, kind, created_at)
		 SELECT ?, NULL, '', 'directory', ?
		 WHERE NOT EXISTS (
		     SELECT 1 FROM entries
		     WHERE user_id = ? AND parent_id IS NULL
		 )`,
		userID,
		time.Now().Unix(),
		userID,
	)
	return err
}

func resolveDirectory(database *sql.DB, userID int64, remotePath string) (int64, error) {
	if err := ensureRoot(database, userID); err != nil {
		return 0, err
	}

	var currentID int64
	if err := database.QueryRow(
		`SELECT entry_id
		 FROM entries
		 WHERE user_id = ? AND parent_id IS NULL AND kind = 'directory'`,
		userID,
	).Scan(&currentID); err != nil {
		return 0, err
	}

	if remotePath == "/" {
		return currentID, nil
	}

	for _, part := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		if part == "" {
			continue
		}

		var nextID int64
		err := database.QueryRow(
			`SELECT entry_id
			 FROM entries
			 WHERE user_id = ?
			   AND parent_id = ?
			   AND name = ?
			   AND kind = 'directory'`,
			userID,
			currentID,
			part,
		).Scan(&nextID)

		if err != nil {
			return 0, err
		}

		currentID = nextID
	}

	return currentID, nil
}

func directoryError(w http.ResponseWriter, err error) {
	if errors.Is(err, sql.ErrNoRows) {
		http.Error(w, "directory not found", http.StatusNotFound)
		return
	}

	http.Error(w, "database or filesystem error", http.StatusInternalServerError)
}

func listDirectory(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	userID, err := authenticatedUserID(r, database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	remotePath, err := cleanRemotePath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	parentID, err := resolveDirectory(database, userID, remotePath)
	if err != nil {
		directoryError(w, err)
		return
	}

	rows, err := database.Query(
		`SELECT name, kind
		 FROM entries
		 WHERE user_id = ? AND parent_id = ?
		 ORDER BY CASE kind WHEN 'directory' THEN 0 ELSE 1 END,
		          name COLLATE NOCASE`,
		userID,
		parentID,
	)
	if err != nil {
		http.Error(w, "could not list directory", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	entries := make([]common.DirectoryEntry, 0)

	for rows.Next() {
		var entry common.DirectoryEntry
		if err := rows.Scan(&entry.Name, &entry.Kind); err != nil {
			http.Error(w, "could not read directory entry", http.StatusInternalServerError)
			return
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		http.Error(w, "could not read directory entries", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entries)
}

func createDirectory(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	userID, err := authenticatedUserID(r, database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	var req common.CreateDirectoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	remotePath, err := cleanRemotePath(req.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if remotePath == "/" {
		http.Error(w, "cannot create root directory", http.StatusBadRequest)
		return
	}

	parentID, err := resolveDirectory(database, userID, pathpkg.Dir(remotePath))
	if err != nil {
		directoryError(w, err)
		return
	}

	name := pathpkg.Base(remotePath)

	var existing int
	if err := database.QueryRow(
		`SELECT COUNT(*)
		 FROM entries
		 WHERE user_id = ? AND parent_id = ? AND name = ?`,
		userID,
		parentID,
		name,
	).Scan(&existing); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	if existing > 0 {
		http.Error(w, "entry already exists", http.StatusConflict)
		return
	}

	if _, err := database.Exec(
		`INSERT INTO entries (user_id, parent_id, name, kind, created_at)
		 VALUES (?, ?, ?, 'directory', ?)`,
		userID,
		parentID,
		name,
		time.Now().Unix(),
	); err != nil {
		http.Error(
			w,
			fmt.Sprintf("could not create directory: %v", err),
			http.StatusConflict,
		)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func removeDirectory(w http.ResponseWriter, r *http.Request, database *sql.DB) {
	userID, err := authenticatedUserID(r, database)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	remotePath, err := cleanRemotePath(r.URL.Query().Get("path"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if remotePath == "/" {
		http.Error(w, "cannot remove root directory", http.StatusBadRequest)
		return
	}

	recursive := r.URL.Query().Get("recursive") == "true"
	force := r.URL.Query().Get("force") == "true"

	parentPath := pathpkg.Dir(remotePath)
	name := pathpkg.Base(remotePath)

	parentID, err := resolveDirectory(database, userID, parentPath)
	if err != nil {
		if force && errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		directoryError(w, err)
		return
	}

	var entryID int64
	err = database.QueryRow(
		`SELECT entry_id
		 FROM entries
		 WHERE user_id = ?
		   AND parent_id = ?
		   AND name = ?
		   AND kind = 'directory'`,
		userID,
		parentID,
		name,
	).Scan(&entryID)

	if err != nil {
		if force && errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		directoryError(w, err)
		return
	}

	if !recursive {
		var children int
		if err := database.QueryRow(
			`SELECT COUNT(*)
			 FROM entries
			 WHERE user_id = ? AND parent_id = ?`,
			userID,
			entryID,
		).Scan(&children); err != nil {
			http.Error(w, "database error", http.StatusInternalServerError)
			return
		}

		if children != 0 {
			http.Error(
				w,
				"directory is not empty (use -r to remove recursively)",
				http.StatusConflict,
			)
			return
		}
	}

	tx, err := database.Begin()
	if err != nil {
		http.Error(w, "could not start removal transaction", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	// Get all entries from the target directory downward.
	// Descendants are ordered before their parents so the foreign key
	// entries.parent_id -> entries.entry_id is respected during deletion.
	entryIDs := []int64{entryID}

	if recursive {
		rows, err := tx.Query(
			`WITH RECURSIVE tree(entry_id, depth) AS (
				SELECT entry_id, 0
				FROM entries
				WHERE entry_id = ? AND user_id = ?

				UNION ALL

				SELECT e.entry_id, tree.depth + 1
				FROM entries e
				INNER JOIN tree ON e.parent_id = tree.entry_id
				WHERE e.user_id = ?
			)
			SELECT entry_id
			FROM tree
			ORDER BY depth DESC`,
			entryID,
			userID,
			userID,
		)
		if err != nil {
			http.Error(
				w,
				fmt.Sprintf("could not enumerate descendants: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

		entryIDs = entryIDs[:0]

		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				http.Error(
					w,
					fmt.Sprintf("could not read descendants: %v", err),
					http.StatusInternalServerError,
				)
				return
			}
			entryIDs = append(entryIDs, id)
		}

		rowsErr := rows.Err()
		rows.Close()

		if rowsErr != nil {
			http.Error(
				w,
				fmt.Sprintf("could not read descendants: %v", rowsErr),
				http.StatusInternalServerError,
			)
			return
		}

		if len(entryIDs) == 0 {
			http.Error(w, "directory not found", http.StatusNotFound)
			return
		}
	}

	// Remove metadata that depends on blocks, uploads, and entries.
	// Replicas reference blocks, so they must be deleted first.
	for _, id := range entryIDs {
		if _, err := tx.Exec(
			`DELETE FROM block_replicas
			 WHERE block_id IN (
			     SELECT block_id
			     FROM blocks
			     WHERE entry_id = ?
			        OR upload_id IN (
			            SELECT upload_id
			            FROM uploads
			            WHERE entry_id = ?
			        )
			 )`,
			id,
			id,
		); err != nil {
			http.Error(
				w,
				fmt.Sprintf("could not remove block replicas: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

		if _, err := tx.Exec(
			`DELETE FROM blocks
			 WHERE entry_id = ?
			    OR upload_id IN (
			        SELECT upload_id
			        FROM uploads
			        WHERE entry_id = ?
			    )`,
			id,
			id,
		); err != nil {
			http.Error(
				w,
				fmt.Sprintf("could not remove blocks: %v", err),
				http.StatusInternalServerError,
			)
			return
		}

		if _, err := tx.Exec(
			`DELETE FROM uploads WHERE entry_id = ?`,
			id,
		); err != nil {
			http.Error(
				w,
				fmt.Sprintf("could not remove uploads: %v", err),
				http.StatusInternalServerError,
			)
			return
		}
	}

	// Delete descendants first and the requested directory last.
	for _, id := range entryIDs {
		if _, err := tx.Exec(
			`DELETE FROM entries
			 WHERE entry_id = ? AND user_id = ?`,
			id,
			userID,
		); err != nil {
			http.Error(
				w,
				fmt.Sprintf("could not remove directory entries: %v", err),
				http.StatusInternalServerError,
			)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(
			w,
			fmt.Sprintf("could not commit directory removal: %v", err),
			http.StatusInternalServerError,
		)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
