package maintenance

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"arhat.dev/pkg/log"
	"arhat.dev/pkg/reconcile"
	corev1 "k8s.io/api/core/v1"
	kubeerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"

	viharaapi "arhat.dev/vihara/pkg/apis/vihara/v1alpha1"
)

func (c *Controller) onMaintenanceJobAdded(obj interface{}) *reconcile.Result {
	var (
		err    error
		mtJob  = obj.(*viharaapi.MaintenanceJob).DeepCopy()
		logger = c.Log.WithFields(log.String("name", mtJob.Name))
	)

	if mtJob.Finished() {
		return nil
	}

	sizeStatus := len(mtJob.Status.Stages)
	sizeSpec := len(mtJob.Spec.Stages)
	if sizeStatus > sizeSpec {
		c.mtJobEventRecorder.Event(mtJob, corev1.EventTypeNormal, "InvalidStatus",
			"status stages are more than spec stages")
		return nil
	}

	for i := sizeStatus; i < sizeSpec; i++ {
		mtJob.Status.Stages = append(mtJob.Status.Stages, viharaapi.MaintenanceJobStageStatus{
			Name: mtJob.Spec.Stages[i].Name,
		})
	}

	if len(mtJob.Status.Stages) > sizeStatus {
		logger.D("updating status to match spec")
		mtJob, err = c.mtJobClient.UpdateStatus(c.Context(), mtJob, metav1.UpdateOptions{})
		if err != nil {
			logger.E("failed to update status", log.Error(err))

			return &reconcile.Result{Err: err}
		}
	}

	logger.D("enqueuing maintenance job for scheduling")
	err = c.enqueueMaintenanceJobForScheduling(mtJob.Namespace, mtJob.Name, 0)
	if err != nil {
		// retry
		logger.E("failed to enqueue maintenance job for scheduling", log.Error(err))
		return &reconcile.Result{Err: err}
	}

	return nil
}

// onMaintenanceJobUpdated checks status of MaintenanceJob and update status of Maintenance if possible
func (c *Controller) onMaintenanceJobUpdated(oldObj, newObj interface{}) *reconcile.Result {
	// TODO: make spec immutable
	// 		once maintenance job added, only status can be updated
	//		and once maintenance job finished, nothing can be updated

	return nil
}

func (c *Controller) onMaintenanceJobDeleting(obj interface{}) *reconcile.Result {
	return nil
}

func (c *Controller) onMaintenanceJobDeleted(obj interface{}) *reconcile.Result {
	return nil
}

func (c *Controller) getMaintenanceJobFromCache(namespace, name string) (*viharaapi.MaintenanceJob, error) {
	key := namespace + "/" + name
	obj, ok, err := c.mtJobInformer.GetIndexer().GetByKey(key)
	if err != nil {
		return nil, fmt.Errorf("failed to search MaintenanceJob in cache: %w", err)
	}

	if !ok {
		return nil, fmt.Errorf("maintenanceJob not found in cache: %s", key)
	}

	return obj.(*viharaapi.MaintenanceJob), nil
}

// nolint:gocyclo
func (c *Controller) scheduleMaintenanceJob(namespace, mtJobName string) error {
	mtJob, err := c.getMaintenanceJobFromCache(namespace, mtJobName)
	if err != nil {
		return err
	}

	switch {
	case mtJob.Status.SucceededAt != nil, mtJob.Status.FailedAt != nil:
		// result was set, not more action
		return nil
	}

	var (
		baseLogger = c.Log.WithFields(log.String("name", mtJobName), log.String("namespace", namespace))
	)

	for i, s := range mtJob.Status.Stages {
		logger := baseLogger.WithFields(log.String("stage", s.Name))

		if s.Failed {
			logger.D("maintenance job has failed, no more action")

			if s.FinishedAt == nil || s.FinishedAt.IsZero() {
				return fmt.Errorf("zero time of failure event not acceptable")
			}

			// fail fast
			mtJob.Status.FailedAt = s.FinishedAt.DeepCopy()
			_, err = c.mtJobClient.UpdateStatus(c.Context(), mtJob, metav1.UpdateOptions{})
			if err != nil {
				logger.I("failed to mark maintenanceJob as failed", log.Error(err))
				return err
			}

			return nil
		}

		if s.Succeeded {
			// schedule next inactive stage
			logger.V("stage succeeded, continue to next stage")

			if s.FinishedAt == nil || s.FinishedAt.IsZero() {
				return fmt.Errorf("zero time of success event not acceptable")
			}

			// requires all stage being finished successfully
			if mtJob.Succeeded() {
				mtJob.Status.SucceededAt = s.FinishedAt.DeepCopy()
				mtJob, err = c.mtJobClient.UpdateStatus(c.Context(), mtJob, metav1.UpdateOptions{})
				if err != nil {
					logger.I("failed to mark maintenanceJob as succeeded", log.Error(err))
					return err
				}
			}

			continue
		}

		// get the next stage spec to be scheduled
		spec := mtJob.Spec.Stages[i]

		if s.Running {
			logger.V("stage is running")
			switch {
			case spec.Drain != nil, spec.Uncordon != nil:
				// TODO: handle drain and uncordon
			default:
				// we do not manage pod execution
				return nil
			}

			// check if job timed out
			if time.Now().UTC().After(s.ScheduledAt.Time.Add(spec.Timeout.Duration)) {
				c.mtJobEventRecorder.Event(
					mtJob, corev1.EventTypeNormal, "StageTimeout", "exceeded time limit for this job")
				// timed out
				mtJob.Status.Stages[i].FinishedAt = newUTCMetav1Now()
				mtJob.Status.Stages[i].Running = false
				mtJob.Status.Stages[i].Succeeded = false
				mtJob.Status.Stages[i].Failed = true

				mtJob.Status.Stages[i].Reason = "Timeout"
				mtJob.Status.Stages[i].Message = "job remains unfinished"

				_, err = c.mtJobClient.UpdateStatus(c.Context(), mtJob, metav1.UpdateOptions{})
				if err != nil {
					return err
				}

				return nil
			}

			switch {
			case spec.Drain != nil:
				err = c.drainNodes(mtJob, spec)
			case spec.Uncordon != nil:
				err = c.uncordonNodes(mtJob, spec)
			}

			if err != nil {
				mtJob.Status.Stages[i].Reason = "TemporaryFailure"
				mtJob.Status.Stages[i].Message = err.Error()

				// best effort
				_, _ = c.mtJobClient.UpdateStatus(c.Context(), mtJob, metav1.UpdateOptions{})

				return err
			}

			mtJob.Status.Stages[i].FinishedAt = newUTCMetav1Now()
			mtJob.Status.Stages[i].Running = false
			mtJob.Status.Stages[i].Succeeded = true
			mtJob.Status.Stages[i].Failed = false

			_, err = c.mtJobClient.UpdateStatus(c.Context(), mtJob, metav1.UpdateOptions{})
			if err != nil {
				return err
			}

			return nil
		}

		// stage job not running, check if delay timed out

		var prevFinishedAt time.Time
		if i == 0 {
			prevFinishedAt = mtJob.CreationTimestamp.Time
		} else {
			if fAt := mtJob.Status.Stages[i-1].FinishedAt; fAt == nil || fAt.IsZero() {
				// unexpected
				logger.E("unexpected no previous stage finish time")
			} else {
				prevFinishedAt = fAt.Time
			}
		}

		if time.Now().UTC().Before(prevFinishedAt.Add(spec.Delay.Duration)) {
			// still need to delay
			return nil
		}

		switch {
		case spec.Drain != nil, spec.Uncordon != nil:
			mtJob.Status.Stages[i].ScheduledAt = newUTCMetav1Now()
			mtJob.Status.Stages[i].Running = true

			mtJob, err = c.mtJobClient.UpdateStatus(c.Context(), mtJob, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		case spec.Pod != nil:
			job := generateJob(mtJob, spec.DeepCopy())

			_, err = c.jobClient.Create(c.Context(), job, metav1.CreateOptions{})
			if err != nil && !kubeerrors.IsAlreadyExists(err) {
				return err
			}

			return nil
		}
	}

	return nil
}

func (c *Controller) uncordonNodes(mtJob *viharaapi.MaintenanceJob, stageSpec viharaapi.MaintenanceStage) error {
	nodes, err := c.getNodesByMaintenanceNodeSelectors(mtJob.Spec.NodeName, stageSpec.Drain.NodeSelector)
	if err != nil {
		return err
	}

	for _, n := range nodes {
		node := n.DeepCopy()
		oldNode := node.DeepCopy()

		var taints []corev1.Taint
		for i, t := range node.Spec.Taints {
			if t.Key == corev1.TaintNodeUnschedulable {
				continue
			}
			taints = append(taints, node.Spec.Taints[i])
		}
		node.Spec.Taints = taints

		if len(node.Spec.Taints) == len(oldNode.Spec.Taints) {
			return nil
		}

		err = c.patchUpdateNode(oldNode, node)
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Controller) drainNodes(mtJob *viharaapi.MaintenanceJob, stageSpec viharaapi.MaintenanceStage) error {
	nodes, err := c.getNodesByMaintenanceNodeSelectors(mtJob.Spec.NodeName, stageSpec.Drain.NodeSelector)
	if err != nil {
		return err
	}

	for _, n := range nodes {
		node := n.DeepCopy()
		oldNode := node.DeepCopy()

		hasExpectedTaints := false

		for _, t := range node.Spec.Taints {
			if t.Effect == corev1.TaintEffectNoSchedule &&
				t.Key == corev1.TaintNodeUnschedulable &&
				t.TimeAdded != nil && t.Value == "" {

				hasExpectedTaints = true
			}
		}

		if !hasExpectedTaints {
			node.Spec.Taints = append(node.Spec.Taints, corev1.Taint{
				Key:       corev1.TaintNodeUnschedulable,
				Value:     "",
				Effect:    corev1.TaintEffectNoSchedule,
				TimeAdded: newUTCMetav1Now(),
			})

			err = c.patchUpdateNode(oldNode, node)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *Controller) getNodesByMaintenanceNodeSelectors(
	nodeName string,
	selectors []viharaapi.MaintenanceNodeSelector,
) ([]*corev1.Node, error) {
	node, err := c.nodeClient.Get(c.Context(), nodeName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	labels := make(map[string]string)
	for _, s := range selectors {
		if s.ValueFrom.NodeFieldRef.FieldPath != "" {
			val, err2 := ExtractNodeFieldPathAsString(node, s.ValueFrom.NodeFieldRef.FieldPath)
			if err2 != nil {
				return nil, err2
			}
			labels[s.Key] = val
		} else {
			labels[s.Key] = s.Value
		}
	}

	nodeList, err := c.nodeClient.List(c.Context(), metav1.ListOptions{
		LabelSelector: createSelectorFromMap(labels),
	})
	if err != nil {
		return nil, err
	}

	var nodes []*corev1.Node
	for _, n := range nodeList.Items {
		nodes = append(nodes, n.DeepCopy())
	}
	return nodes, nil
}

func (c *Controller) enqueueMaintenanceJobForScheduling(namespace, mtJobName string, delay time.Duration) error {
	mtJob, err := c.getMaintenanceJobFromCache(namespace, mtJobName)
	if err != nil {
		return err
	}

	finishedCount := 0
	for _, s := range mtJob.Status.Stages {
		if s.Failed {
			// this maintenance job has failed, no more scheduling
			// user can trigger rescheduling by deleting this one
			return nil
		}

		if s.Running {
			// stage running, no need to check following stages
			break
		}

		if s.Succeeded {
			finishedCount++
		}
	}

	// all stages finished, no more scheduling
	if finishedCount == len(mtJob.Spec.Stages) {
		return nil
	}

	err = c.mtJobScheduleTQ.OfferWithDelay(mtJobName, nil, delay)
	if err != nil {
		return err
	}

	return nil
}

func (c *Controller) patchUpdateNode(oldObj, newObj *corev1.Node) error {
	oldData, err := json.Marshal(oldObj)
	if err != nil {
		return fmt.Errorf("failed to marshal old object: %w", err)
	}

	newData, err := json.Marshal(newObj)
	if err != nil {
		return fmt.Errorf("failed to marshal new object: %w", err)
	}

	patchData, err := strategicpatch.CreateTwoWayMergePatch(oldData, newData, &corev1.Node{})
	if err != nil {
		return fmt.Errorf("failed to generate patch data: %w", err)
	}

	_, err = c.nodeClient.Patch(c.Context(), newObj.Name,
		types.StrategicMergePatchType, patchData, metav1.PatchOptions{})
	return err
}

func generateMaintenanceJob(
	nodeName, machineID string,
	mt *viharaapi.Maintenance,
	nodeSpecificEnv []corev1.EnvVar,
	nodeSpecificEnvFrom []corev1.EnvFromSource,
	mtSpecSnapshot *viharaapi.MaintenanceSpec,
	namespace string,
	defaultStageTimeout time.Duration,
) *viharaapi.MaintenanceJob {
	var (
		stageSpecs []viharaapi.MaintenanceStage
	)

	for _, stage := range mtSpecSnapshot.Stages {
		spec := stage.DeepCopy()

		if spec.Timeout.Duration == 0 {
			spec.Timeout = metav1.Duration{Duration: defaultStageTimeout}
		}

		// nolint:gocritic
		switch {
		case spec.Pod != nil:
			// allow override of target node by specifying node affinity, nodeSelector or nodeName
			switch {
			case spec.Pod.Spec.Affinity != nil && spec.Pod.Spec.Affinity.NodeAffinity != nil,
				spec.Pod.Spec.NodeName != "",
				spec.Pod.Spec.NodeSelector != nil:
				// do not change toleration in this case
			default:
				spec.Pod.Spec.NodeName = nodeName
				spec.Pod.Spec.Tolerations = mtSpecSnapshot.Tolerations
			}

			spec.Pod.Spec.RestartPolicy = corev1.RestartPolicyNever

			for i := range spec.Pod.Spec.InitContainers {
				spec.Pod.Spec.InitContainers[i].Env = mergeEnv(
					spec.Pod.Spec.InitContainers[i].Env, nodeSpecificEnv)
				spec.Pod.Spec.InitContainers[i].EnvFrom = mergeEnvFrom(
					spec.Pod.Spec.InitContainers[i].EnvFrom, nodeSpecificEnvFrom)
			}

			for i := range spec.Pod.Spec.Containers {
				spec.Pod.Spec.Containers[i].Env = mergeEnv(
					spec.Pod.Spec.Containers[i].Env, nodeSpecificEnv)
				spec.Pod.Spec.Containers[i].EnvFrom = mergeEnvFrom(
					spec.Pod.Spec.Containers[i].EnvFrom, nodeSpecificEnvFrom)
			}
		}

		stageSpecs = append(stageSpecs, *spec)
	}

	return &viharaapi.MaintenanceJob{
		ObjectMeta: metav1.ObjectMeta{
			// kubernetes doesn't allow dot(`.`) in node name, but you can use it in other names,
			// that's why we use it as a separator
			Name:      fmt.Sprintf("%s.%s", mt.Name, nodeName),
			Namespace: namespace,
			Labels:    mt.Labels,
			// Add controller owner reference to prevent deletion of Maintenance
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(mt, viharaapi.SchemeGroupVersion.WithKind("Maintenance")),
			},
		},
		Spec: viharaapi.MaintenanceJobSpec{
			NodeName:  nodeName,
			MachineID: machineID,
			Stages:    stageSpecs,
		},
	}
}

func mergeEnv(a, b []corev1.EnvVar) []corev1.EnvVar {
	var result []corev1.EnvVar

	bm := make(map[string]int)
	for i, e := range b {
		bm[e.Name] = i
	}

	for i, e := range a {
		v, ok := bm[e.Name]
		if !ok {
			result = append(result, a[i])
		} else {
			result = append(result, b[v])
			delete(bm, e.Name)
		}
	}

	var remains []int
	for _, i := range bm {
		remains = append(remains, i)
	}

	sort.Ints(remains)
	for _, i := range remains {
		result = append(result, b[i])
	}

	return result
}

func mergeEnvFrom(a, b []corev1.EnvFromSource) []corev1.EnvFromSource {
	var result []corev1.EnvFromSource

	bm := make(map[string]int)
	for i, e := range b {
		switch {
		case e.ConfigMapRef != nil:
			bm["cm"+e.ConfigMapRef.Name+e.Prefix] = i
		case e.SecretRef != nil:
			bm["sec"+e.SecretRef.Name+e.Prefix] = i
		}
	}

	for i, e := range a {
		var key string
		switch {
		case e.ConfigMapRef != nil:
			key = "cm" + e.ConfigMapRef.Name + e.Prefix
		case e.SecretRef != nil:
			key = "sec" + e.SecretRef.Name + e.Prefix
		}

		v, ok := bm[key]
		if !ok {
			result = append(result, a[i])
		} else {
			result = append(result, b[v])
			delete(bm, key)
		}
	}

	var remains []int
	for _, i := range bm {
		remains = append(remains, i)
	}

	sort.Ints(remains)
	for _, i := range remains {
		result = append(result, b[i])
	}

	return result
}
