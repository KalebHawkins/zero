A fake project for the end-to-end test, laid out like the platform's content.

project.json         the project file (content/projects/<id>.json)
life-1/, life-2/     the two stages. A stage's starter/ holds only what the stage adds.
                     life-1/solution/ is the whole project after stage 1: the fake API
                     sends it as the reference for life-2. life-2/solution/ holds only
                     grid.go, which the test writes as the learner's answer.
wrap/                a plain exercise. Stage 2 copies its wrap.go with zero use.

go.mod is stored as go.mod.txt, as in hello-world.
