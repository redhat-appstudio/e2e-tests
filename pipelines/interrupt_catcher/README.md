# IC rotation Cron triggered pipeline

This pipeline triggers an Appstudio QE Interrupt catcher roration. It's scheduled to run every Thursday at 10 AM.

## Prerequisities
* Cluster with Openshift Pipelines installed (tested with Pipelines 1.7.2).
* Existing `appstudio-qe` namespace
* Slack App OAuth token with at least `usergroups:read` and `usergroups:write` scopes.
* Opaque secret `slack-token` in `appstudio-qe` namespace with `token` data. This is slack app OAuth token with at least `usergroups:read` and `usergroups:write` scopes.
```
oc create secret generic slack-token --from-literal="token=<TOKEN>"
```

## Deployment
* Create secret in `appstudio-qe` namespace with Slack token:
```
oc create secret generic slack-token --from-literal="token=<TOKEN>"
```
* Deploy rest of the resoureces: 
```
cd pipelines/interrupt_catcher
kubectl apply -f .
```
This should create all the remaining resources.
## How to add/remove user to rotation
Users in rotation and their order is defined in configmap `cm_people_list.yaml` in form of `base64` encoded json.
To obtain the json run
```
yq ".binaryData.people-list" cm_people_list.yaml |base64 -d
```
Json is in format 
```
[
    {"username": "USER1", "id": "USER1_SLACK_ID"},
    {"username": "USER2", "id": "USER2_SLACK_ID"}
]
```
After you've made your changes, encode json back (`cat myJson.json |base64 -w 0`) and replace `binaryData` in `cm_people_list.yaml`
## How to manually trigger another run of the pipeline
Sometimes we would like to skip somebody in rotation. To do thata we might run the pipeline again manually. We can do this by creating `Job` from our `CronJob`
```
oc create job --from=cronjob/interrupt-catcher ic-manual-1
```
After the pipeline has run, you can delete the job:
```
oc delete job/ic-manual-1
```
## Slack API call to change directly
```
curl -X POST -H "Authorization: Bearer $TOKEN" "https://slack.com/api/usergroups.users.update?usergroup=${USERGROUP_ID}&users=${id}"
```
Where `${id}` is slack user ID and `${USERGROUP_ID}` is our IC usergroup's ID (`S03PD4MV58W`)
