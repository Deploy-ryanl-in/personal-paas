#!/usr/bin/env python3
"""Generate schemas, pinned Dockerfiles and examples from reviewed pins."""
import json, pathlib, base64
root=pathlib.Path(__file__).resolve().parents[1]; templates=root.parent/'templates'
pins=json.loads((root/'pins.json').read_text());images=pins['images']
def obj(props,required=None): return {'type':'object','additionalProperties':False,'properties':props,'required':required if required is not None else list(props)}
string={'type':'string'}; env={'type':'object','propertyNames':{'pattern':'^[A-Z][A-Z0-9_]{0,127}$'},'additionalProperties':{'type':'string','maxLength':4096}}
resource=obj({'memoryMiB':{'type':'integer','minimum':32,'maximum':768},'cpu':{'type':'number','exclusiveMinimum':0,'maximum':1},'pids':{'type':'integer','minimum':16,'maximum':256}})
service=obj({'type':{'enum':['web','worker','postgres','redis']},'build':obj({'context':string,'dockerfile':string,'publicArgs':env},['context','dockerfile']),'image':string,'port':{'type':'integer','minimum':1024,'maximum':65535},'domain':string,'routing':obj({'sharedGroup':{'type':'string','pattern':'^[a-z][a-z0-9-]{0,39}$'},'paths':{'type':'array','uniqueItems':True,'maxItems':16,'items':{'type':'string','pattern':'^/[^?# \\`]*$'}}},['sharedGroup']),'health':obj({'path':{'type':'string','pattern':'^/[^?#]*$'},'status':{'type':'integer','minimum':200,'maximum':299}}),'resources':resource,'environment':env,'secretRefs':env,'volumes':{'type':'array','maxItems':8,'items':obj({'name':{'type':'string','pattern':'^[a-z][a-z0-9-]{0,39}$'},'target':string})},'dependsOn':{'type':'array','uniqueItems':True,'items':string}},['type','resources'])
service['allOf']=[{'if':{'properties':{'type':{'enum':['web','worker']}}},'then':{'required':['build'],'not':{'required':['image']}}},{'if':{'properties':{'type':{'const':'web'}}},'then':{'required':['port','domain','health']},'else':{'not':{'anyOf':[{'required':['port']},{'required':['domain']},{'required':['health']},{'required':['routing']}]}}},{'if':{'properties':{'type':{'enum':['postgres','redis']}}},'then':{'required':['image','volumes'],'not':{'required':['build']}}}]
schema=obj({'schemaVersion':{'const':1},'name':string,'state':{'enum':['present','absent']},'deployBranch':string,'services':{'type':'object','minProperties':1,'maxProperties':8,'propertyNames':{'pattern':'^[a-z][a-z0-9-]{0,39}$'},'additionalProperties':service}});schema.update({'$schema':'https://json-schema.org/draft/2020-12/schema','$id':'https://raw.githubusercontent.com/Deploy-ryanl-in/personal-paas/main/schema/paas.v1.json','title':'Personal PaaS v1'})
(root/'schema').mkdir(exist_ok=True);(root/'schema/paas.v1.json').write_text(json.dumps(schema,indent=2)+'\n')
for kind,port,memory in [('aspnet',8080,192),('nextjs',3000,256)]:
    directory=templates/kind
    config={'schemaVersion':1,'name':'auto','state':'present','deployBranch':'main','services':{'web':{'type':'web','build':{'context':'.','dockerfile':'Dockerfile'},'port':port,'domain':'auto','health':{'path':'/healthz','status':200},'resources':{'memoryMiB':memory,'cpu':0.5,'pids':128},'environment':{},'secretRefs':{}}}}
    (directory/'paas.json').write_text(json.dumps(config,indent=2)+'\n')
    (directory/'schema').mkdir(exist_ok=True);(directory/'schema/paas.v1.json').write_text(json.dumps(schema,indent=2)+'\n')
    (directory/'.vscode').mkdir(exist_ok=True);(directory/'.vscode/settings.json').write_text(json.dumps({'json.schemas':[{'fileMatch':['paas.json','examples/*.json'],'url':'./schema/paas.v1.json'}]},indent=2)+'\n')
    config['services']['postgres']={'type':'postgres','image':images['postgres'],'resources':{'memoryMiB':256,'cpu':0.3,'pids':64},'environment':{'POSTGRES_USER':'app','POSTGRES_DB':'app'},'secretRefs':{'POSTGRES_PASSWORD':'POSTGRES_PASSWORD'},'volumes':[{'name':'postgres-data','target':'/var/lib/postgresql'}]}
    config['services']['redis']={'type':'redis','image':images['redis'],'resources':{'memoryMiB':64,'cpu':0.2,'pids':32},'secretRefs':{'REDIS_PASSWORD':'REDIS_PASSWORD'},'volumes':[{'name':'redis-data','target':'/data'}]}
    config['services']['web']['dependsOn']=['postgres','redis']
    config['services']['web']['environment']={'DATABASE_HOST':'postgres','DATABASE_PORT':'5432','DATABASE_NAME':'app','DATABASE_USER':'app','REDIS_HOST':'redis','REDIS_PORT':'6379','CONFIG_PATH':'/data/config.json'}
    config['services']['web']['volumes']=[{'name':'app-config','target':'/data'}]
    config['services']['web']['secretRefs']={'DATABASE_PASSWORD':'POSTGRES_PASSWORD','REDIS_PASSWORD':'REDIS_PASSWORD','API_TOKEN':'API_TOKEN'}
    (directory/'examples').mkdir(exist_ok=True);(directory/'examples/postgres-redis.json').write_text(json.dumps(config,indent=2)+'\n')
    config['services']['web']['resources']['memoryMiB']=128
    config['services']['worker']={'type':'worker','build':{'context':'examples/worker','dockerfile':'examples/worker/Dockerfile'},'resources':{'memoryMiB':128,'cpu':0.1,'pids':32},'dependsOn':['redis'],'environment':{'REDIS_HOST':'redis'},'secretRefs':{'REDIS_PASSWORD':'REDIS_PASSWORD'}}
    (directory/'examples/multi-container.json').write_text(json.dumps(config,indent=2)+'\n')
    worker=directory/'examples/worker';worker.mkdir(exist_ok=True)
    (worker/'Dockerfile').write_text(f"FROM {images['node']}\nWORKDIR /app\nCOPY worker.mjs .\nUSER 10001:10001\nCMD [\"node\",\"worker.mjs\"]\n")
    (worker/'worker.mjs').write_text("""import net from 'node:net';
const command=(...args)=>'*'+args.length+'\\r\\n'+args.map(value=>'$'+Buffer.byteLength(value)+'\\r\\n'+value+'\\r\\n').join('');
setInterval(()=>{const s=net.connect(6379,process.env.REDIS_HOST||'redis',()=>{if(process.env.REDIS_PASSWORD)s.write(command('AUTH',process.env.REDIS_PASSWORD));s.write(command('INCR','worker-counter'));});let reply='';s.on('data',b=>{reply+=b.toString();if(reply.includes(':')){console.log('worker increment succeeded');s.end();}});s.setTimeout(3000,()=>s.destroy());s.on('error',()=>console.error('Redis unavailable'));},5000);
""")
    (directory/'.github').mkdir(exist_ok=True)
    (directory/'.github/dependabot.yml').write_text(f"version: 2\nupdates:\n  - package-ecosystem: {'nuget' if kind=='aspnet' else 'npm'}\n    directory: /\n    schedule:\n      interval: weekly\n    open-pull-requests-limit: 5\n  - package-ecosystem: github-actions\n    directory: /\n    schedule:\n      interval: weekly\n  - package-ecosystem: docker\n    directory: /\n    schedule:\n      interval: weekly\n")
(templates/'aspnet/Dockerfile').write_text(f'''FROM {images['dotnet-sdk']} AS build
WORKDIR /source
COPY global.json Directory.Build.props App.slnx ./
COPY src/App/App.csproj src/App/packages.lock.json ./src/App/
RUN dotnet restore src/App/App.csproj --locked-mode
COPY src/ ./src/
RUN dotnet publish src/App/App.csproj -c Release --no-restore -o /out /p:UseAppHost=false
FROM {images['dotnet-runtime']} AS runtime
WORKDIR /app
COPY --from=build /out ./
ENV ASPNETCORE_HTTP_PORTS=8080 DOTNET_EnableDiagnostics=0 DOTNET_BUNDLE_EXTRACT_BASE_DIR=/tmp/dotnet
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["dotnet", "App.dll"]
''')
(templates/'nextjs/Dockerfile').write_text(f'''FROM {images['node']} AS dependencies
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
FROM {images['node']} AS build
WORKDIR /app
COPY --from=dependencies /app/node_modules ./node_modules
COPY . .
ARG NEXT_PUBLIC_APP_LABEL=Personal-PaaS
ENV NEXT_PUBLIC_APP_LABEL=$NEXT_PUBLIC_APP_LABEL NEXT_TELEMETRY_DISABLED=1
RUN npm run build
FROM {images['node']} AS runtime
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 HOSTNAME=0.0.0.0 PORT=3000 NODE_OPTIONS=--max-old-space-size=160
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public
COPY cache-handler.cjs docker-entrypoint.sh ./
RUN mkdir -p .next/cache && rm -rf .next/cache/images && ln -s /tmp/next-image-cache .next/cache/images && chmod 755 docker-entrypoint.sh
USER 10001:10001
EXPOSE 3000
ENTRYPOINT ["/app/docker-entrypoint.sh"]
''')
(templates/'nextjs/public').mkdir(exist_ok=True)
(templates/'nextjs/public/sample.png').write_bytes(base64.b64decode('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jXioAAAAASUVORK5CYII='))
policy={'audience':'https://deploy.ryanl.in','domain':'ryanl.in','owners':[{'id':'93820487','login':'RyanStanLin'},{'id':'337720882','login':'Deploy-ryanl-in'}],'repositoryAllowlist':[],'workflows':{},'databaseImages':{images['postgres']:'postgres',images['redis']:'redis'},'helperImage':images['alpine']}
(root/'deploy').mkdir(exist_ok=True);(root/'deploy/policy.example.json').write_text(json.dumps(policy,indent=2)+'\n')
print('Generated strict schema, manifests, examples, pinned Dockerfiles and policy.')
