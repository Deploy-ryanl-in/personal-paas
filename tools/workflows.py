#!/usr/bin/env python3
import pathlib,json,sys
root=pathlib.Path(__file__).resolve().parents[1];templates=root.parent/'templates';pins=json.loads((root/'pins.json').read_text());a=pins['actions'];sha=sys.argv[1] if len(sys.argv)>1 else 'PLATFORM_COMMIT_SHA'
def action(n):return n+'@'+a[n]
workflows=root/'.github/workflows';workflows.mkdir(parents=True,exist_ok=True)
transport=(root/'tools/transport.py').read_text();embedded='\n'.join('          '+line for line in transport.splitlines())
(workflows/'release.yml').write_text('''name: Reusable image release and OIDC deployment
on:
  workflow_call:
    inputs:
      platform-sha:
        required: true
        type: string
    secrets:
      PAAS_SECRETS:
        required: false
permissions: {}
concurrency:
  group: paas-${{ github.repository_id }}-release
  cancel-in-progress: false
jobs:
  prepare:
    runs-on: ubuntu-24.04
    permissions:
      contents: read
    outputs:
      matrix: ${{ steps.config.outputs.matrix }}
      enabled: ${{ steps.config.outputs.enabled }}
      has-builds: ${{ steps.config.outputs.has-builds }}
    steps:
      - uses: '''+action('actions/checkout')+'''
        with:
          persist-credentials: false
          path: application
      - uses: '''+action('actions/checkout')+'''
        with:
          repository: Deploy-ryanl-in/personal-paas
          ref: ${{ inputs.platform-sha }}
          persist-credentials: false
          path: platform
      - uses: '''+action('actions/setup-go')+'''
        with:
          go-version-file: platform/go.mod
          cache-dependency-path: platform/go.sum
      - name: Validate declaration
        id: config
        env:
          REPOSITORY_ID: ${{ github.repository_id }}
          REPOSITORY_OWNER: ${{ github.repository_owner }}
          REPOSITORY_NAME: ${{ github.event.repository.name }}
          EVENT_NAME: ${{ github.event_name }}
          REF: ${{ github.ref }}
          TEMPLATE: ${{ github.event.repository.is_template }}
        working-directory: platform
        run: |
          go run ./cmd/paas validate --file ../application/paas.json --repository-id "$REPOSITORY_ID" --owner "$REPOSITORY_OWNER" --name "$REPOSITORY_NAME" > config.json
          python3 - <<'PY'
          import json,os
          config=json.load(open('config.json'))
          enabled=os.environ['EVENT_NAME']=='push' and os.environ['REF']=='refs/heads/'+config['deployBranch'] and os.environ['TEMPLATE']!='true'
          with open(os.environ['GITHUB_OUTPUT'],'a') as f:
              f.write('matrix='+json.dumps(config['matrix'])+'\\n')
              f.write('enabled='+str(enabled).lower()+'\\n')
              f.write('has-builds='+str(bool(config['matrix']['include'])).lower()+'\\n')
          PY
  build:
    needs: prepare
    if: needs.prepare.outputs.enabled == 'true' && needs.prepare.outputs.has-builds == 'true'
    strategy:
      fail-fast: false
      max-parallel: 2
      matrix: ${{ fromJSON(needs.prepare.outputs.matrix) }}
    runs-on: ubuntu-24.04
    permissions:
      contents: read
      packages: write
    steps:
      - uses: '''+action('actions/checkout')+'''
        with:
          persist-credentials: false
      - uses: '''+action('docker/setup-buildx-action')+'''
      - uses: '''+action('docker/login-action')+'''
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - name: Public build arguments
        id: args
        env:
          PUBLIC_ARGS: ${{ toJSON(matrix.publicArgs) }}
        run: |
          python3 - <<'PY'
          import json,os
          args=json.loads(os.environ['PUBLIC_ARGS'] or '{}') or {}
          with open(os.environ['GITHUB_OUTPUT'],'a') as f:
              f.write('args<<PUBLIC_BUILD_ARGS\\n')
              for k,v in args.items():
                  if '\\n' in v or '\\r' in v:raise ValueError('multiline build arg prohibited')
                  f.write(k+'='+v+'\\n')
              f.write('PUBLIC_BUILD_ARGS\\n')
          PY
      - uses: '''+action('docker/build-push-action')+'''
        id: image
        with:
          context: ${{ matrix.context }}
          file: ${{ matrix.dockerfile }}
          platforms: linux/amd64
          push: true
          tags: ${{ matrix.image }}:${{ github.sha }}
          build-args: ${{ steps.args.outputs.args }}
          provenance: mode=max
          labels: |
            org.opencontainers.image.source=https://github.com/${{ github.repository }}
            org.opencontainers.image.revision=${{ github.sha }}
            in.ryanl.paas.managed=true
            in.ryanl.paas.repository=${{ github.repository_id }}
      - name: Record digest
        env:
          SERVICE: ${{ matrix.service }}
          IMAGE: ${{ matrix.image }}
          DIGEST: ${{ steps.image.outputs.digest }}
        run: |
          python3 - <<'PY'
          import os,json
          with open(os.environ['SERVICE']+'.json','w') as f:json.dump({'service':os.environ['SERVICE'],'image':os.environ['IMAGE'],'digest':os.environ['DIGEST']},f)
          PY
      - uses: '''+action('actions/upload-artifact')+'''
        with:
          name: image-${{ matrix.service }}
          path: ${{ matrix.service }}.json
          retention-days: 3
  deploy:
    needs: [prepare, build]
    if: always() && needs.prepare.outputs.enabled == 'true' && needs.prepare.result == 'success' && (needs.build.result == 'success' || needs.build.result == 'skipped')
    runs-on: ubuntu-24.04
    timeout-minutes: 25
    permissions:
      id-token: write
      contents: read
      actions: read
    steps:
      - uses: '''+action('actions/download-artifact')+'''
        if: needs.prepare.outputs.has-builds == 'true'
        with:
          pattern: image-*
          merge-multiple: true
          path: images
      - name: Authenticated declarative deployment
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          PAAS_SECRETS: ${{ secrets.PAAS_SECRETS }}
          PAAS_MODE: deploy
        run: |
          python3 - <<'PY'
'''+embedded+'''
          PY
''')
(workflows/'operate.yml').write_text('''name: Reusable fixed PaaS operations
on:
  workflow_call:
    inputs:
      action: {required: true, type: string}
      release-id: {required: false, type: string, default: ''}
      service: {required: false, type: string, default: web}
      image: {required: false, type: string, default: ''}
      backup-id: {required: false, type: string, default: ''}
      confirm: {required: false, type: string, default: ''}
permissions: {}
jobs:
  operate:
    runs-on: ubuntu-24.04
    timeout-minutes: 25
    permissions:
      id-token: write
      contents: read
    steps:
      - name: Execute fixed operation
        env:
          PAAS_MODE: operation
          OP_ACTION: ${{ inputs.action }}
          OP_RELEASE: ${{ inputs.release-id }}
          OP_SERVICE: ${{ inputs.service }}
          OP_IMAGE: ${{ inputs.image }}
          OP_BACKUP: ${{ inputs.backup-id }}
          OP_CONFIRM: ${{ inputs.confirm }}
        run: |
          python3 - <<'PY'
'''+embedded+'''
          PY
      - uses: '''+action('actions/upload-artifact')+'''
        if: inputs.action == 'download-backup'
        with:
          name: encrypted-backup
          path: backup.tar.age
          retention-days: 1
''')
(workflows/'ci.yml').write_text('''name: Controller CI
on: [push, pull_request]
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - uses: '''+action('actions/checkout')+'''
        with: {persist-credentials: false}
      - uses: '''+action('actions/setup-go')+'''
        with: {go-version-file: go.mod}
      - run: go test -race ./...
      - run: go vet ./...
      - run: test -z "$(gofmt -l .)"
      - run: go build ./cmd/paas
''')
for kind in ['aspnet','nextjs']:
    folder=templates/kind/'.github/workflows';folder.mkdir(parents=True,exist_ok=True)
    setup=('''      - uses: '''+action('actions/setup-dotnet')+'''
        with: {dotnet-version: '10.0.401'}
      - run: dotnet restore --locked-mode
      - run: dotnet format --no-restore --verify-no-changes
      - run: dotnet build --no-restore -c Release
      - run: dotnet test --no-build --no-restore -c Release
''' if kind=='aspnet' else '''      - uses: '''+action('actions/setup-node')+'''
        with: {node-version: '24.21.0', cache: npm}
      - run: npm ci
      - run: npm run lint
      - run: npm run typecheck
      - run: npm test
      - run: npm run build
      - run: npx playwright install --with-deps chromium
      - run: npm run test:smoke
''')
    (folder/'ci.yml').write_text('''name: CI and deployment
on:
  push:
    branches: ['**']
  pull_request:
permissions:
  contents: read
jobs:
  test:
    runs-on: ubuntu-24.04
    steps:
      - uses: '''+action('actions/checkout')+'''
        with: {persist-credentials: false}
'''+setup+'''  release:
    needs: test
    if: github.event_name == 'push' && !github.event.repository.is_template
    permissions:
      contents: read
      packages: write
      actions: read
      id-token: write
    uses: Deploy-ryanl-in/personal-paas/.github/workflows/release.yml@'''+sha+'''
    with:
      platform-sha: '''+sha+'''
    secrets:
      PAAS_SECRETS: ${{ secrets.PAAS_SECRETS }}
''')
    (folder/'operations.yml').write_text('''name: PaaS operations
on:
  workflow_dispatch:
    inputs:
      action:
        description: Fixed platform operation
        type: choice
        options: [status, history, logs, redeploy, rollback, stop, backup, backups, download-backup, restore, database-upgrade]
        default: status
      service: {description: Named service, default: web, type: string}
      release-id: {description: Rollback release ID, type: string}
      image: {description: Approved database image digest, type: string}
      backup-id: {description: Backup ID, type: string}
      confirm: {description: 'Restore requires RESTORE <repository ID>', type: string}
permissions:
  contents: read
  id-token: write
jobs:
  operate:
    uses: Deploy-ryanl-in/personal-paas/.github/workflows/operate.yml@'''+sha+'''
    with:
      action: ${{ inputs.action }}
      service: ${{ inputs.service }}
      release-id: ${{ inputs.release-id }}
      image: ${{ inputs.image }}
      backup-id: ${{ inputs.backup-id }}
      confirm: ${{ inputs.confirm }}
''')
print('Generated pinned workflows; template platform ref: '+sha)
