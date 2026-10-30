import sys,subprocess,os
sys.path.insert(0,'/workspace/bot-dialog-handoff');import results_harness as h
c=h.cfg
dsn='postgres://'+c['user']+':'+c['password']+'@127.0.0.1:'+str(c['port'])+'/memoh_saved_result_recovery_20261030?sslmode=disable'
r=subprocess.run(['go','test','./cmd/internal/core','-run','^TestSavedResultRecoveryPostgres$','-count=1','-v'],env={**os.environ,'MEMOH_NATIVE_QUEUE_TEST_DSN':dsn},stdout=open('/workspace/bot-dialog-handoff/saved-result-recovery/pg.log','w'),stderr=subprocess.STDOUT)
print('targeted PostgreSQL test exit',r.returncode)
raise SystemExit(r.returncode)
