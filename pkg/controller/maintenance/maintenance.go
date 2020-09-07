package maintenance

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"arhat.dev/pkg/envhelper"
	"arhat.dev/pkg/hashhelper"
	"arhat.dev/pkg/log"
	"arhat.dev/pkg/queue"
	"arhat.dev/pkg/reconcile"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	corev1helper "k8s.io/kubernetes/pkg/apis/core/v1/helper"
	"k8s.io/kubernetes/pkg/fieldpath"

	viharaapi "arhat.dev/vihara/pkg/apis/vihara/v1alpha1"
	"arhat.dev/vihara/pkg/constant"
)

// Maintenance resource objects has four phases
//	- Phase Created: created by user, waiting to be admitted by controller
//	- Phase Admitted: admitted by controller, changes to spec.schedule will be ignored
//	- Phase Scheduled: scheduled by controller, changes to spec will be ignored
//	- Phase Finished: finished by selected nodes, changes to the object will be rejected, but can be deleted

// onMaintenanceAdded will only be called once for each maintenance object in the cluster by default, no matter it
// has already there for a long time or was newly created by user, so we do not do extensive validation here, most
// validation jobs are carried out in onMaintenanceUpdated
//
// It will validates the maintenance object and ensure every object has spec.schedule.{since,until} been set,
// once the object has all the required fields, the controller will admit it by setting
// status.{admittedBy, admittedAt}
func (c *Controller) onMaintenanceAdded(obj interface{}) *reconcile.Result {
	var (
		err    error
		mt     = obj.(*viharaapi.Maintenance).DeepCopy()
		logger = c.Log.WithFields(log.String("name", mt.Name))
	)

	logger.V("onMaintenanceAdded")

	if mt.Admitted() {
		// this maintenance object has been admitted before, schedule now
		logger.V("already admitted")
		return &reconcile.Result{NextAction: queue.ActionUpdate}
	}

	if !mt.CanBeScheduledIn(c.jobNamespace) {
		logger.V("not intended for this job namespace",
			log.Strings("mtNS", mt.Spec.TargetNamespaces),
			log.String("jobNS", c.jobNamespace))
		return nil
	}

	// ensure since
	since := mt.Spec.Schedule.Since
	if since == nil || since.IsZero() {
		since = &mt.ObjectMeta.CreationTimestamp
	}

	delay := mt.Spec.Schedule.Delay.Duration
	if delay < 1 {
		delay = c.config.Maintenance.Schedule.DefaultDelay
	}
	scheduleSince := metav1.NewTime(since.Add(delay).UTC())
	mt.Status.Admitted.ScheduleSince = &scheduleSince

	// ensure until
	until := mt.Spec.Schedule.Until
	if until == nil || until.IsZero() {
		until = mt.Status.Admitted.ScheduleSince.DeepCopy()
		dur := mt.Spec.Schedule.Duration.Duration
		if dur < 1 {
			dur = c.config.Maintenance.Schedule.DefaultDuration
		}
		until = &metav1.Time{Time: until.Add(dur)}
	}

	scheduleUntil := metav1.NewTime(until.UTC())
	mt.Status.Admitted.ScheduleUntil = &scheduleUntil

	// ensure since < until
	if !mt.Status.Admitted.ScheduleSince.Time.Before(mt.Status.Admitted.ScheduleUntil.Time) {
		// `since` must be ahead of `until`, otherwise reject the maintenance
		c.mtEventRecorder.Eventf(mt, corev1.EventTypeWarning, "InvalidSchedule",
			"since %q should be earlier than until %q",
			mt.Status.Admitted.ScheduleSince.Time.Format(time.RFC3339),
			mt.Status.Admitted.ScheduleUntil.Time.Format(time.RFC3339))
		return nil
	}

	mt.Status.Admitted.At = newUTCMetav1Now()
	mt.Status.Admitted.By = *refThisPod()

	logger.D("updating maintenance status for admitting")
	mt, err = c.mtClient.UpdateStatus(c.Context(), mt, metav1.UpdateOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			// this object has been deleted
			logger.V("maintenance not found, no more action", log.Error(err))
			return nil
		}

		logger.E("failed to admit maintenance object", log.Error(err))

		// this is not the error caused by user, retry to admit this maintenance object
		return &reconcile.Result{Err: err}
	}

	c.mtEventRecorder.Event(mt, corev1.EventTypeNormal, "Admitted", "valid schedule spec")

	// we just admitted this object, need to enqueue it to get it scheduled in future
	return &reconcile.Result{NextAction: queue.ActionUpdate}
}

// onMaintenanceAdded enqueues valid maintenance objects for scheduling, and `valid` here means:
//	- being admitted before
//	- not expired (now < spec.schedule.until)
//	- has non empty spec.{nodeSelector, stages}
func (c *Controller) onMaintenanceUpdated(oldObj, newObj interface{}) *reconcile.Result {
	var (
		err    error
		now    = time.Now()
		newMt  = newObj.(*viharaapi.Maintenance).DeepCopy()
		logger = c.Log.WithFields(log.String("name", newMt.Name))
	)

	logger.V("onMaintenanceUpdated")

	if newMt.Finished() {
		// this maintenance has been finished, no more operations required
		return nil
	}

	if !newMt.CanBeScheduledIn(c.jobNamespace) {
		return nil
	}

	if !newMt.Admitted() {
		// this maintenance object has NOT been admitted
		c.mtEventRecorder.Event(newMt, corev1.EventTypeNormal, "NotAdmitted",
			"will not be scheduled until admitted")
		// go back to admit it if possible
		return &reconcile.Result{NextAction: queue.ActionAdd}
	}

	if newMt.Expired(now) {
		return nil
	}

	if newMt.Scheduled() {
		if hashhelper.Sha256SumHex([]byte(newMt.Status.Scheduled.SpecSnapshot)) !=
			newMt.Status.Scheduled.SpecSnapshotHash.Sha256Hex {
			c.mtEventRecorder.Event(newMt, corev1.EventTypeNormal, "InvalidSnapshot",
				"snapshot doesn't match its sha256 hash")
			return nil
		}
	}

	if len(newMt.Spec.NodeSelector) == 0 {
		c.mtEventRecorder.Event(newMt, corev1.EventTypeNormal, "InvalidSpec",
			"must provide valid spec.nodeSelector")
		return nil
	}

	if len(newMt.Spec.Stages) == 0 {
		c.mtEventRecorder.Event(newMt, corev1.EventTypeNormal, "InvalidSpec",
			"must provide valid spec.stages")
		return nil
	}

	for _, env := range newMt.Spec.Env {
		if err = env.Validate(); err != nil {
			c.mtEventRecorder.Event(newMt, corev1.EventTypeNormal, "InvalidSpec", err.Error())
			return nil
		}
	}

	for _, envFrom := range newMt.Spec.EnvFrom {
		if err = envFrom.Validate(); err != nil {
			c.mtEventRecorder.Event(newMt, corev1.EventTypeNormal, "InvalidSpec", err.Error())
			return nil
		}
	}

	logger.D("enqueuing maintenance for scheduling")
	err = c.enqueueMaintenanceForScheduling(newMt.Name, c.config.Maintenance.Schedule.PollInterval)
	if err != nil {
		c.mtEventRecorder.Event(newMt, corev1.EventTypeNormal, "EnqueueFailed",
			"unable to schedule this maintenance")
		return &reconcile.Result{Err: err}
	}
	c.mtEventRecorder.Event(newMt, corev1.EventTypeNormal, "Enqueued",
		"schedule of the maintenance has been planned")

	return nil
}

// onMaintenanceDeleting keeps Maintenance objects if they are being scheduled but not finished
func (c *Controller) onMaintenanceDeleting(obj interface{}) *reconcile.Result {
	var (
		err    error
		mt     = obj.(*viharaapi.Maintenance).DeepCopy()
		logger = c.Log.WithFields(log.String("name", mt.Name))
	)

	logger.V("onMaintenanceDeleting")

	if !mt.CanBeScheduledIn(c.jobNamespace) {
		return nil
	}

	switch {
	case mt.Scheduled() && !mt.Finished():
		// MUST not allow deleting a maintenance already scheduled
		c.mtEventRecorder.Event(mt, corev1.EventTypeWarning, "DeletionNotAllowed",
			"unable to delete maintenance under execution")
		return nil
	case mt.Admitted() && !mt.Scheduled():
		_, _ = c.mtScheduleTQ.Remove(mt.Name)
	}

	// this maintenance object has NOT been admitted, delete it immediately
	logger.I("deleting maintenance")
	err = c.mtClient.Delete(c.Context(), mt.Name, *metav1.NewDeleteOptions(0))
	if err != nil {
		c.Log.I("failed to delete maintenance", log.Error(err))
		return &reconcile.Result{Err: err}
	}

	return nil
}

func (c *Controller) onMaintenanceDeleted(obj interface{}) *reconcile.Result {
	var (
		mt = obj.(*viharaapi.Maintenance).DeepCopy()
	)

	if !mt.CanBeScheduledIn(c.jobNamespace) {
		return nil
	}

	c.Log.I("maintenance deleted", log.String("name", mt.Name), log.Any("data", mt))

	return nil
}

func (c *Controller) getMaintenanceFromCache(name string) (*viharaapi.Maintenance, error) {
	obj, ok, err := c.mtInformer.GetIndexer().GetByKey(name)
	if err != nil {
		return nil, fmt.Errorf("failed to load latest Maintenance from cache: %w", err)
	}

	if !ok {
		return nil, fmt.Errorf("maintenance not found in cache: %s", name)
	}

	return obj.(*viharaapi.Maintenance), nil
}

func (c *Controller) enqueueMaintenanceForScheduling(mtName string, delay time.Duration) error {
	mt, err := c.getMaintenanceFromCache(mtName)
	if err != nil {
		return err
	}

	var (
		logger = c.Log.WithFields(log.String("name", mtName))
		now    = time.Now().UTC()
	)

	if mt.Canceled() {
		return nil
	}

	if mt.Finished() {
		return nil
	}

	if mt.Expired(now) {
		// expired but not finished
		mt.Status.Finished.At = newUTCMetav1Now()
		mt.Status.Finished.By = *refThisPod()

		mt, err = c.mtClient.UpdateStatus(c.Context(), mt, metav1.UpdateOptions{})
		if err != nil {
			logger.E("failed to set to finished", log.Error(err))

			goto doEnqueue
		}

		c.mtEventRecorder.Event(mt, corev1.EventTypeNormal, "ScheduleFailed",
			"missed schedule time frame")
		return nil
	}

	// this maintenance is still valid, so it is able to be scheduled

doEnqueue:
	// just try to reduce the unnecessary schedule check
	if _, found := c.mtScheduleTQ.Find(mt.Name); found {
		logger.V("skipped enqueuing of duplicated object")
		return nil
	}

	if mt.Started(now) {
		// this maintenance has started, enqueue using delay
		err = c.mtScheduleTQ.OfferWithDelay(mt.Name, nil, delay)
	} else {
		// maintenance has not been started, enqueue using start timestamp
		// and add snapshot for this maintenance resource object
		err = c.mtScheduleTQ.OfferWithTime(mt.Name, nil, mt.Status.Admitted.ScheduleSince.Time)
	}

	if err != nil {
		logger.E("failed to enqueue maintenance", log.Error(err))
		return err
	}

	return nil
}

// scheduleMaintenance do the following jobs
//	- make sure the maintenance object is immutable since then (by setting status.scheduledAt to now)
//	- create MaintenanceJob(s) for this Maintenance
// nolint:gocyclo
func (c *Controller) scheduleMaintenance(mtName string) error {
	mt, err := c.getMaintenanceFromCache(mtName)
	if err != nil {
		return err
	}

	if mt.Canceled() {
		// canceled (resource marked deleted)
		return nil
	}

	var (
		logger = c.Log.WithFields(log.String("name", mt.Name), log.String("op", "scheduling"))
		now    = time.Now().UTC()
	)

	if mt.Expired(now) {
		// will be marked as finished when doing enqueuing (next step)
		c.mtEventRecorder.Event(mt, corev1.EventTypeNormal, "Expired", "will finish in no time")
		return nil
	}

	if !mt.Started(now) {
		// it's too early to schedule it will be enqueued soon (next step)
		return nil

		//return c.enqueueMaintenanceForScheduling(mt.Name, c.config.Maintenance.Schedule.PollInterval)
	}

	// time to schedule!

	if !mt.Scheduled() {
		// first time to be scheduled, mark scheduled
		specJSON, err2 := json.Marshal(mt.Spec)
		if err2 != nil {
			return fmt.Errorf("failed to encode spec of maintenance %q to json: %w", mt.Name, err2)
		}

		mt.Status.Scheduled.At = newUTCMetav1Now()
		mt.Status.Scheduled.By = *refThisPod()
		mt.Status.Scheduled.SpecSnapshot = string(specJSON)
		mt.Status.Scheduled.SpecSnapshotHash.Sha256Hex = hashhelper.Sha256SumHex(specJSON)

		logger.D("updating maintenance status for scheduling")
		mt, err2 = c.mtClient.UpdateStatus(c.Context(), mt, metav1.UpdateOptions{})
		if err2 != nil {
			if errors.IsNotFound(err2) {
				// this object has been deleted
				logger.V("maintenance not found, no more action", log.Error(err2))
				return nil
			}

			logger.I("failed to patch update maintenance for scheduling", log.Error(err))
			return err2
		}
	}

	// scheduled, validate it's information
	if mt.Status.Scheduled.SpecSnapshot == "" || mt.Status.Scheduled.SpecSnapshotHash.Sha256Hex == "" {
		c.mtEventRecorder.Event(mt, corev1.EventTypeNormal, "InvalidObject",
			"spec snapshot and its sha256 hash not set for scheduled maintenance")
		return fmt.Errorf("invalid object at scheduled stage")
	}

	if hashhelper.Sha256SumHex([]byte(mt.Status.Scheduled.SpecSnapshot)) !=
		mt.Status.Scheduled.SpecSnapshotHash.Sha256Hex {
		c.mtEventRecorder.Event(mt, corev1.EventTypeNormal, "InvalidObject",
			"spec snapshot does not match its sha256 hash")
		return fmt.Errorf("failed to validate spec snapshot")
	}

	// get spec from snapshot
	spec := new(viharaapi.MaintenanceSpec)
	err = json.Unmarshal([]byte(mt.Status.Scheduled.SpecSnapshot), spec)
	if err != nil {
		return fmt.Errorf("failed to decode spec from json: %w", err)
	}

	// nodes with maintenance job not finished (not fulfill the dependency requirements in individual mode)
	unavailableNodes := make(map[string]struct{})
	for _, dep := range spec.Schedule.Depends {
		depMt, err2 := c.getMaintenanceFromCache(dep.Name)
		if err2 != nil {
			logger.I(fmt.Sprintf("dependent maintenance not found %q", dep.Name), log.Error(err2))
			return nil
		}

		// non-individual dependency means wait until its scheduling finished
		if !dep.Individual && !depMt.Finished() {
			logger.I(fmt.Sprintf("dependent maintenance %q not finished", dep.Name))
			return nil
		}

		// maintenanceJob name is in `<maintenance name>.<node name>` format
		depMtJobNamePrefix := fmt.Sprintf("%s.", dep.Name)

		for _, namespacedName := range c.mtJobInformer.GetIndexer().ListKeys() {
			parts := strings.SplitN(namespacedName, "/", 2)
			if len(parts) != 2 {
				continue
			}

			namespace := parts[0]
			depMtJobName := parts[1]
			if !strings.HasPrefix(depMtJobName, depMtJobNamePrefix) {
				// all possibly related maintenanceJobs by name prefix are
				// accepted for next round filtering
				continue
			}

			depMtJob, err2 := c.getMaintenanceJobFromCache(namespace, depMtJobName)
			if err2 != nil {
				return fmt.Errorf("failed to check maintenance job in cache: %w", err2)
			}

			// filter out unrelated maintenanceJobs
			match := false
			for _, ref := range depMtJob.OwnerReferences {
				switch {
				case ref.Name != dep.Name:
				case ref.Kind != "Maintenance":
				case ref.UID != depMt.UID:
				case ref.BlockOwnerDeletion == nil || !*ref.BlockOwnerDeletion:
				case ref.Controller == nil || !*ref.Controller:
				default:
					match = true
				}
				if match {
					// reduce unnecessary loop (usually only one iteration)
					break
				}
			}

			if !match {
				// we don't have dependency on this
				continue
			}

			if !dep.Individual {
				// non-individual dependency requires all maintenance to be in same condition, if not
				// will not be scheduled
				switch dep.Condition {
				case "", viharaapi.MaintenanceConditionSucceeded:
					if !(depMtJob.Finished() && depMtJob.Succeeded()) {
						// dependency requirement not met, do not schedule
						logger.I(
							fmt.Sprintf("condition of dependent maintenance job %q doesn't fit requirement",
								depMtJob.Name),
							log.String("expected", dep.Condition), log.String("actual", "succeeded"))
						return nil
					}
				case viharaapi.MaintenanceConditionFailed:
					if !(depMtJob.Finished() && depMtJob.Failed()) {
						// dependency requirement not met, do not schedule
						logger.I(
							fmt.Sprintf("condition of dependent maintenance job %q doesn't fit requirement",
								depMtJob.Name),
							log.String("expected", dep.Condition), log.String("actual", "failed"))
						return nil
					}
				default:
					logger.I(fmt.Sprintf("unknown condition %q", dep.Condition))
					return nil
				}
			}

			switch dep.Condition {
			case "", viharaapi.MaintenanceConditionSucceeded:
				if !(depMtJob.Finished() && depMtJob.Succeeded()) {
					unavailableNodes[depMtJob.Spec.NodeName] = struct{}{}
				}
			case viharaapi.MaintenanceConditionFailed:
				if !(depMtJob.Finished() && depMtJob.Failed()) {
					unavailableNodes[depMtJob.Spec.NodeName] = struct{}{}
				}
			default:
				logger.I(fmt.Sprintf("unknown condition %q", dep.Condition))
				return nil
			}
		}
	}

	var listOpts metav1.ListOptions
	if len(spec.NodeSelector) > 0 {
		listOpts.LabelSelector = labels.SelectorFromSet(spec.NodeSelector).String()
	}

	selectedNodes := sets.NewString(spec.Nodes...)

	labels.SelectorFromSet(spec.NodeSelector)
	nodeList, err := c.nodeClient.List(c.Context(), listOpts)
	if err != nil {
		logger.I("failed to get nodes selected")
		return err
	}

	// create node specific maintenance jobs
	var newMtJobs []*viharaapi.MaintenanceJob
	for i, node := range nodeList.Items {
		if selectedNodes.Len() != 0 && !selectedNodes.Has(node.Name) {
			continue
		}

		if _, unavail := unavailableNodes[node.Name]; unavail {
			continue
		}

		_, found := corev1helper.FindMatchingUntoleratedTaint(node.Spec.Taints, spec.Tolerations, nil)
		if found {
			continue
		}

		// TODO: ignore existing combinations of node and maintenance objects
		machineID := node.Status.NodeInfo.MachineID
		if machineID == "" {
			continue
		}

		var (
			nodeSpecificEnv     []corev1.EnvVar
			nodeSpecificEnvFrom []corev1.EnvFromSource
		)
		for _, e := range spec.Env {
			envVar, err2 := CreatePodEnvVarFromNodeSpecificEnv(&nodeList.Items[i], e)
			if err2 != nil {
				return err2
			}

			nodeSpecificEnv = append(nodeSpecificEnv, *envVar)
		}

		for _, e := range spec.EnvFrom {
			envFrom, err2 := CreatePodEnvSourceFromNodeSpecificEnvFrom(&nodeList.Items[i], e)
			if err2 != nil {
				return err2
			}

			nodeSpecificEnvFrom = append(nodeSpecificEnvFrom, *envFrom)
		}

		newMtJobs = append(newMtJobs,
			generateMaintenanceJob(node.Name, machineID, mt,
				nodeSpecificEnv, nodeSpecificEnvFrom,
				spec, c.jobNamespace,
				c.config.Maintenance.Jobs.DefaultStageTimeout),
		)
	}

	baseLogger := logger
creationLoop:
	for _, newMtJob := range newMtJobs {
		name := newMtJob.Name
		for i := uint64(1); i < math.MaxUint64; i++ {
			if i != 1 {
				// name will be like <maintenance name>.<node name>{,"-2...N"}
				name = fmt.Sprintf("%s-%s", newMtJob.Name, strconv.FormatUint(i, 16))
			}

			oldMtJob, err2 := c.getMaintenanceJobFromCache(newMtJob.Namespace, name)
			if err2 == nil {
				// probably already created, check machineID
				if oldMtJob.Spec.MachineID == newMtJob.Spec.MachineID {
					// has same machineID, same machine, do not create again
					continue creationLoop
				}
			} else {
				// no such maintenance, create new one
				break
			}
		}

		logger := baseLogger.WithFields(log.String("node", newMtJob.Spec.NodeName))
		logger.D("creating MaintenanceJob")

		newMtJob.Name = name
		_, err = c.mtJobClient.Create(c.Context(), newMtJob, metav1.CreateOptions{})
		if err != nil {
			if errors.IsAlreadyExists(err) {
				// we may be creating one maintenance job for multiple times in some cases
				continue
			}

			logger.E("failed to create MaintenanceJob", log.Error(err))
			continue
		}

		logger.V("created MaintenanceJob")
	}

	return nil
}

func CreatePodEnvSourceFromNodeSpecificEnvFrom(
	node *corev1.Node,
	nodeEnvFrom viharaapi.NodeSpecificEnvFromSource,
) (*corev1.EnvFromSource, error) {
	envFrom := &corev1.EnvFromSource{
		Prefix:       nodeEnvFrom.Prefix,
		ConfigMapRef: nil,
		SecretRef:    nil,
	}

	targetRef := nodeEnvFrom.ConfigMapRef
	if targetRef == nil {
		targetRef = nodeEnvFrom.SecretRef
	}

	var err error
	name := targetRef.Name

	if name == "" {
		name, err = ExtractNodeFieldPathAsString(node, targetRef.NameFrom.NodeFieldRef.FieldPath)
		if err != nil {
			return nil, err
		}
	}

	if targetRef.NameFormat != "" {
		name = fmt.Sprintf(targetRef.NameFormat, name)
	}

	switch {
	case nodeEnvFrom.SecretRef != nil:
		envFrom.SecretRef = &corev1.SecretEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: name,
			},
			Optional: nil,
		}
	case nodeEnvFrom.ConfigMapRef != nil:
		envFrom.ConfigMapRef = &corev1.ConfigMapEnvSource{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: name,
			},
			Optional: nil,
		}
	}

	return envFrom, nil
}

func CreatePodEnvVarFromNodeSpecificEnv(
	node *corev1.Node,
	nodeEnv viharaapi.NodeSpecificEnvVar,
) (*corev1.EnvVar, error) {
	envVar := &corev1.EnvVar{
		Name: nodeEnv.Name,
		ValueFrom: &corev1.EnvVarSource{
			FieldRef:         nil,
			ResourceFieldRef: nil,
			ConfigMapKeyRef:  nil,
			SecretKeyRef:     nil,
		},
	}

	targetRef := nodeEnv.ValueFrom.ConfigMapKeyRef
	if targetRef == nil {
		targetRef = nodeEnv.ValueFrom.SecretKeyRef
	}

	var err error
	name := targetRef.Name
	key := targetRef.Key

	if name == "" {
		name, err = ExtractNodeFieldPathAsString(node, targetRef.NameFrom.NodeFieldRef.FieldPath)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve node specific name for env %s: %w", nodeEnv.Name, err)
		}
	}

	if key == "" {
		key, err = ExtractNodeFieldPathAsString(node, targetRef.KeyFrom.NodeFieldRef.FieldPath)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve node specific key for env %s: %w", nodeEnv.Name, err)
		}
	}

	if targetRef.NameFormat != "" {
		name = fmt.Sprintf(targetRef.NameFormat, name)
	}

	if targetRef.KeyFormat != "" {
		key = fmt.Sprintf(targetRef.KeyFormat, key)
	}

	switch {
	case nodeEnv.ValueFrom.ConfigMapKeyRef != nil:
		envVar.ValueFrom.ConfigMapKeyRef = &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: name,
			},
			Key:      key,
			Optional: nil,
		}
	case nodeEnv.ValueFrom.SecretKeyRef != nil:
		envVar.ValueFrom.SecretKeyRef = &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: name,
			},
			Key:      name,
			Optional: nil,
		}
	}

	return envVar, nil
}

func ExtractNodeFieldPathAsString(node *corev1.Node, fieldPath string) (string, error) {
	var ret string
	switch fieldPath {
	case "status.nodeInfo.bootID":
		ret = node.Status.NodeInfo.BootID
	case "status.nodeInfo.systemUUID":
		ret = node.Status.NodeInfo.SystemUUID
	case "status.nodeInfo.machineID":
		ret = node.Status.NodeInfo.MachineID
	case "node.spec.providerID":
		ret = node.Spec.ProviderID
	default:
		var err error
		ret, err = fieldpath.ExtractFieldPathAsString(node, fieldPath)
		if err != nil {
			return "", err
		}
	}

	return ret, nil
}

func newUTCMetav1Now() *metav1.Time {
	t := metav1.NewTime(time.Now().UTC())
	return &t
}

func refThisPod() *viharaapi.ResourceReference {
	return &viharaapi.ResourceReference{
		Namespace: envhelper.ThisPodNS(),
		Name:      envhelper.ThisPodName(),
		UID:       types.UID(constant.ThisPodUID()),
	}
}

func createSelectorFromMap(m map[string]string) string {
	var selectors []fields.Selector
	for k, v := range m {
		selectors = append(selectors, fields.OneTermEqualSelector(k, v))
	}

	return fields.AndSelectors(selectors...).String()
}
