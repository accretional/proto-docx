# proto-docx

## Instructions

Make sure you create a setup.sh, build.sh, test.sh, and LET_IT_RIP.sh that contain all project setup scripts/commands used - NEVER build/test/run the code in this repo outside of these scripts, NEVER commit or push without running these either. Make them idempotent so that each build.sh can run setup.sh and skip things already set up, each test.sh can run build.sh, each LET_IT_RIP runs test.sh

use go1.26

1. Import https://github.com/accretional/mime-proto/blob/main/pb/proto/openformat/v1/xml.proto to xml.proto and related xml-specific logic
2. Make sure the encoder/decoder use cases work fully e2e with unit tests
3. Create data/ directory with multiple xml files exhibiting various different aspects of the format that we can use for testing. Create some programmatically using the protos and others just as regular old xml files.
4. Create a testing/validation/ directory running a suite of tests (for now just one) across all the data/
5. Create a testing/fuzz/ directory running fuzzing tests
6. Create a testing/benchmarks directory running benchmarks across the data
7. Document any discrepancies or irregularities in the testing in testing/README.md, as well as the overall strategy/setup
8. Augment this README.md in ## NEXT STEPS with anything important you find, any irregularities in the file format, bad implementations, missing functionality, etc.
9. Write a docs/about.md explaining this project, with examples, in a way someone might actually use it (eg with rss). Use github.com/accretional/chromerpc to take screenshots as you walk through a demo of a real xml file. Prepare to embed these images in about.md in the github markdown format.
