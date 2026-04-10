// migrate_attributes — one-time migration script.
//
// Moves /attributes and /attribute_sets from Firestore root collections
// into tenant-scoped sub-collections:
//   /tenants/{tenantID}/attributes/{id}
//   /tenants/{tenantID}/attribute_sets/{id}
//
// Usage:
//   cd backend
//   go run cmd/migrate_attributes/main.go --tenant=<tenant_id> --dry-run
//   go run cmd/migrate_attributes/main.go --tenant=<tenant_id>
//
// The --dry-run flag prints every document that would be moved without
// writing or deleting anything. Always run dry-run first.
//
// Prerequisites: GOOGLE_APPLICATION_CREDENTIALS must be set (or the service
// account key path set via --credentials), and the process must have read/write
// access to Firestore for the target project.

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

const projectID = "marketmate-486116"

func main() {
	tenantID := flag.String("tenant", "", "Target tenant ID to migrate data into (required)")
	dryRun := flag.Bool("dry-run", false, "Print what would be moved without making any changes")
	credentials := flag.String("credentials", "", "Path to service account JSON (optional — defaults to GOOGLE_APPLICATION_CREDENTIALS)")
	flag.Parse()

	if *tenantID == "" {
		fmt.Fprintln(os.Stderr, "ERROR: --tenant flag is required")
		fmt.Fprintln(os.Stderr, "Usage: go run cmd/migrate_attributes/main.go --tenant=<tenant_id> [--dry-run]")
		os.Exit(1)
	}

	if *dryRun {
		fmt.Println("═══════════════════════════════════════════════════")
		fmt.Println("  DRY RUN — no data will be written or deleted")
		fmt.Println("═══════════════════════════════════════════════════")
	}

	fmt.Printf("Project:   %s\n", projectID)
	fmt.Printf("Tenant:    %s\n", *tenantID)
	fmt.Printf("Dry run:   %v\n\n", *dryRun)

	ctx := context.Background()

	var opts []option.ClientOption
	if *credentials != "" {
		opts = append(opts, option.WithCredentialsFile(*credentials))
	}

	client, err := firestore.NewClient(ctx, projectID, opts...)
	if err != nil {
		log.Fatalf("Failed to create Firestore client: %v", err)
	}
	defer client.Close()

	attrMigrated, attrSetMigrated := 0, 0
	var attrErrors, attrSetErrors []string

	// ── Migrate /attributes ────────────────────────────────────────────────────

	fmt.Println("─── Migrating /attributes ───────────────────────────────────────────")
	attrIter := client.Collection("attributes").Documents(ctx)
	for {
		doc, err := attrIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("Error reading /attributes: %v", err)
		}

		data := doc.Data()
		docID := doc.Ref.ID
		srcPath := fmt.Sprintf("attributes/%s", docID)
		dstPath := fmt.Sprintf("tenants/%s/attributes/%s", *tenantID, docID)

		// Stamp tenant_id onto the document
		data["tenant_id"] = *tenantID

		if *dryRun {
			fmt.Printf("  [DRY RUN] MOVE  %s\n          → %s\n", srcPath, dstPath)
			attrMigrated++
			continue
		}

		// Write to new path
		_, err = client.Doc(dstPath).Set(ctx, data)
		if err != nil {
			msg := fmt.Sprintf("  ERROR writing %s: %v", dstPath, err)
			fmt.Println(msg)
			attrErrors = append(attrErrors, msg)
			continue
		}

		// Delete from old path
		_, err = client.Doc(srcPath).Delete(ctx)
		if err != nil {
			msg := fmt.Sprintf("  ERROR deleting %s: %v", srcPath, err)
			fmt.Println(msg)
			attrErrors = append(attrErrors, msg)
			continue
		}

		fmt.Printf("  ✓  %s  →  %s\n", srcPath, dstPath)
		attrMigrated++
	}

	// ── Migrate /attribute_sets ────────────────────────────────────────────────

	fmt.Println("\n─── Migrating /attribute_sets ───────────────────────────────────────")
	setIter := client.Collection("attribute_sets").Documents(ctx)
	for {
		doc, err := setIter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			log.Fatalf("Error reading /attribute_sets: %v", err)
		}

		data := doc.Data()
		docID := doc.Ref.ID
		srcPath := fmt.Sprintf("attribute_sets/%s", docID)
		dstPath := fmt.Sprintf("tenants/%s/attribute_sets/%s", *tenantID, docID)

		data["tenant_id"] = *tenantID

		if *dryRun {
			fmt.Printf("  [DRY RUN] MOVE  %s\n          → %s\n", srcPath, dstPath)
			attrSetMigrated++
			continue
		}

		_, err = client.Doc(dstPath).Set(ctx, data)
		if err != nil {
			msg := fmt.Sprintf("  ERROR writing %s: %v", dstPath, err)
			fmt.Println(msg)
			attrSetErrors = append(attrSetErrors, msg)
			continue
		}

		_, err = client.Doc(srcPath).Delete(ctx)
		if err != nil {
			msg := fmt.Sprintf("  ERROR deleting %s: %v", srcPath, err)
			fmt.Println(msg)
			attrSetErrors = append(attrSetErrors, msg)
			continue
		}

		fmt.Printf("  ✓  %s  →  %s\n", srcPath, dstPath)
		attrSetMigrated++
	}

	// ── Summary ────────────────────────────────────────────────────────────────

	fmt.Println("\n═══════════════════════════════════════════════════")
	if *dryRun {
		fmt.Printf("  DRY RUN COMPLETE\n")
		fmt.Printf("  Would migrate: %d attributes, %d attribute sets\n", attrMigrated, attrSetMigrated)
		fmt.Println("\n  Re-run without --dry-run to apply.")
	} else {
		fmt.Printf("  MIGRATION COMPLETE\n")
		fmt.Printf("  Attributes migrated:     %d\n", attrMigrated)
		fmt.Printf("  Attribute sets migrated: %d\n", attrSetMigrated)
		if len(attrErrors)+len(attrSetErrors) > 0 {
			fmt.Printf("  Errors:                  %d\n", len(attrErrors)+len(attrSetErrors))
			fmt.Println("\n  Failed operations:")
			for _, e := range append(attrErrors, attrSetErrors...) {
				fmt.Println(" ", e)
			}
			os.Exit(1)
		}
	}
	fmt.Println("═══════════════════════════════════════════════════")
}
