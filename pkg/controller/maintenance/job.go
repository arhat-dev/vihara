package maintenance

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"arhat.dev/pkg/log"
	"arhat.dev/pkg/reconcile"

	viharaapi "arhat.dev/vihara/pkg/apis/vihara/v1alpha1"
	"arhat.dev/vihara/pkg/constant"
)

func (c *Controller) onJobAdded(obj interface{}) *reconcile.Result {
	return nil
}

// onJobUpdated will trigger updates for MaintenanceJob status
func (c *Controller) onJobUpdated(oldObj, newObj interface{}) *reconcile.Result {
	var (
		err      error
		newJob   = newObj.(*batchv1.Job)
		ownerRef = metav1.GetControllerOf(newJob)
	)

	if ownerRef == nil || ownerRef.Kind != "MaintenanceJob" {
		// not managed by us
		return nil
	}

	logger := c.Log.WithFields(log.String("namespace", newJob.Namespace), log.String("name", ownerRef.Name))

	if len(newJob.Labels) == 0 {
		// job must contain at least one label for stage name
		return nil
	}

	stageName, ok := newJob.Labels[constant.LabelMaintenanceStage]
	if !ok {
		// job must contain the label for stage name
		return nil
	}

	mtJobObj, ok, _ := c.mtJobInformer.GetIndexer().GetByKey(newJob.Namespace + "/" + ownerRef.Name)
	if !ok {
		// no such maintenance job, should not happen in the same namespace
		logger.I("maintenanceJob not found for kubernetes job")
		return nil
	}

	mtJob, ok := mtJobObj.(*viharaapi.MaintenanceJob)
	if !ok {
		logger.I("failed to convert object to MaintenanceJob", log.Any("obj", mtJobObj))
		return nil
	}
	mtJob = mtJob.DeepCopy()

	needToUpdate := false
	for i, stageStatus := range mtJob.Status.Stages {
		if stageStatus.Name != stageName {
			continue
		}

		oldStageStatus := stageStatus.DeepCopy()

		if oldStageStatus.FinishedAt != nil && !oldStageStatus.FinishedAt.IsZero() {
			break
		}

		newStageStatus := &viharaapi.MaintenanceJobStageStatus{
			Name:        stageName,
			ScheduledAt: newJob.CreationTimestamp.DeepCopy(),
			Running:     newJob.Status.Active == 1,
			Succeeded:   newJob.Status.Succeeded == 1,
			Failed:      newJob.Status.Failed == 1,
		}

		if newStageStatus.Succeeded || newStageStatus.Failed {
			if newJob.Status.CompletionTime != nil {
				newStageStatus.FinishedAt = newJob.Status.CompletionTime.DeepCopy()
			} else {
				newStageStatus.FinishedAt = newUTCMetav1Now()
			}
		}

		// get last condition of the job to set reason and message
		if size := len(newJob.Status.Conditions); size > 0 {
			newStageStatus.Reason = newJob.Status.Conditions[size-1].Reason
			newStageStatus.Message = newJob.Status.Conditions[size-1].Message
		}

		switch {
		case oldStageStatus.ScheduledAt.Equal(newStageStatus.ScheduledAt):
		case oldStageStatus.Running == newStageStatus.Running:
		case oldStageStatus.Succeeded == newStageStatus.Succeeded:
		case oldStageStatus.Failed == newStageStatus.Failed:
		case oldStageStatus.FinishedAt.Equal(oldStageStatus.FinishedAt):
		case oldStageStatus.Reason == newStageStatus.Reason:
		case oldStageStatus.Message == newStageStatus.Message:
		default:
			needToUpdate = true
		}

		mtJob.Status.Stages[i] = *newStageStatus
	}

	if !needToUpdate {
		return nil
	}

	logger.V("updating maintenanceJob status")
	_, err = c.mtJobClient.UpdateStatus(c.Context(), mtJob, metav1.UpdateOptions{})
	if err != nil {
		logger.I("failed to update maintenanceJob status", log.Error(err))
		return &reconcile.Result{Err: err}
	}

	return nil
}

func (c *Controller) onJobDeleting(obj interface{}) *reconcile.Result {
	return nil
}

func (c *Controller) onJobDeleted(obj interface{}) *reconcile.Result {
	return nil
}

// generateJob is used to create job for stages with pod template
func generateJob(mtJob *viharaapi.MaintenanceJob, stage *viharaapi.MaintenanceStage) *batchv1.Job {
	one := int32(1)

	labels := make(map[string]string)
	for k, v := range mtJob.Labels {
		labels[k] = v
	}

	// ensure job contains stage name
	labels[constant.LabelMaintenanceStage] = stage.Name

	timeout := int64(stage.Timeout.Seconds())

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			// TODO: the name will be <maintenance name>.<node name>.<stage name>
			//		 by this way will definitely exceed the size limit of the names in some cases,
			//		 we may need to find a better way to name it
			Name:      fmt.Sprintf("%s.%s", mtJob.Name, stage.Name),
			Namespace: mtJob.Namespace,
			Labels:    labels,

			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(mtJob, viharaapi.SchemeGroupVersion.WithKind("MaintenanceJob")),
			},
		},
		Spec: batchv1.JobSpec{
			Parallelism:           &one,
			Completions:           &one,
			ActiveDeadlineSeconds: &timeout,
			// TODO: set backoff limit to some reasonable value (default to 6)
			//BackoffLimit:          &one,

			Template: *stage.Pod.DeepCopy(),
		},
	}

	return job
}
