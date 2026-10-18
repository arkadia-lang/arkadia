# Workflow

- main に直接コミットしない。作業ごとにブランチを切り、PR 経由でマージする。
- PR を作成したら `gh pr checks --watch` で CI 結果まで確認する。失敗したら直して再確認。
