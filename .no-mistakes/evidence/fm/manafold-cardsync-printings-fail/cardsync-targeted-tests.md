# Targeted cardsync test run (branch bcf2924)

## No-DB regression + spool tests
```
=== RUN   TestBulkDownload_SpoolDecouplesIngestFromTheConnection
    download_test.go:50: pre-fix pattern failed as expected: unexpected EOF
--- PASS: TestBulkDownload_SpoolDecouplesIngestFromTheConnection (5.55s)
=== RUN   TestDownloadToTemp_IdleConnectionFailsAndCleansUp
--- PASS: TestDownloadToTemp_IdleConnectionFailsAndCleansUp (0.15s)
=== RUN   TestDownloadToTemp_TotalDeadlineFailsSlowTrickleAndCleansUp
--- PASS: TestDownloadToTemp_TotalDeadlineFailsSlowTrickleAndCleansUp (0.25s)
=== RUN   TestDownloadToTemp_RoundTripsGzipBody
--- PASS: TestDownloadToTemp_RoundTripsGzipBody (0.00s)
=== RUN   TestOpenBulkFile_InflatesGzippedJSONL
--- PASS: TestOpenBulkFile_InflatesGzippedJSONL (0.00s)
=== RUN   TestOpenBulkFile_NonGzipIsAnError
--- PASS: TestOpenBulkFile_NonGzipIsAnError (0.00s)
PASS
ok  	manafold-backend/internal/cardsync	(cached)
```

## DB-integration: COPY-merge parity + no-temp-file end-to-end + dev seed
```
=== RUN   TestRun_IngestsFixture_DerivesFields
--- PASS: TestRun_IngestsFixture_DerivesFields (0.06s)
=== RUN   TestRun_EndToEnd_ScryfallShapedGzipManifest
    endtoend_test.go:108: mirror populated: OracleUpserted=4 PrintsUpserted=2 PrintsSkipped=1; autocomplete("Manafold Test") -> [Manafold Test Goblin Boss Manafold Test Walker Manafold Test Rat Swarm Manafold Test Septet]
--- PASS: TestRun_EndToEnd_ScryfallShapedGzipManifest (0.05s)
=== RUN   TestRun_DevSeed
--- PASS: TestRun_DevSeed (0.28s)
PASS
ok  	manafold-backend/internal/cardsync	0.384s
```
