package s2irun

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"text/template"

	"k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/util/yaml"

	devopsv1alpha1 "github.com/kubesphere/s2ioperator/pkg/apis/devops/v1alpha1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ConfigDataKey     = "data"
	TaintKey          = "node.kubernetes.io/ci"
	NodeAffinityKey   = "node-role.kubernetes.io/worker"
	NodeAffinityValue = "ci"
)

func (r *ReconcileS2iRun) NewRegularRole(roleName, namespace string) *v1.Role {
	cr := &v1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      roleName,
			Namespace: namespace,
		},
		Rules: []v1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch", "update", "patch"},
			},
		},
	}

	return cr
}

func (r *ReconcileS2iRun) NewServiceAccount(saName string, namespace string) *corev1.ServiceAccount {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      saName,
			Namespace: namespace,
		},
	}
	return sa
}

func (r *ReconcileS2iRun) NewRoleBinding(name, roleName, saName, namespace string) *v1.RoleBinding {
	roleBinding := &v1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subjects: []v1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      saName,
				Namespace: namespace,
			},
		},
		RoleRef: v1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     roleName,
		},
	}

	return roleBinding
}

func (r *ReconcileS2iRun) NewConfigMap(instance *devopsv1alpha1.S2iRun, config devopsv1alpha1.S2iConfig, template *devopsv1alpha1.UserDefineTemplate) (*corev1.ConfigMap, error) {
	if template != nil {
		t := &devopsv1alpha1.S2iBuilderTemplate{}
		err := r.Get(context.TODO(), types.NamespacedName{Name: template.Name}, t)
		if err != nil {
			return nil, err
		}
		if template.BuilderImage != "" {
			config.BuilderImage = template.BuilderImage
		} else {
			config.BuilderImage = t.Spec.DefaultBaseImage
		}
		if len(template.Parameters) > 0 {
			for _, p := range template.Parameters {
				e := p.ToEnvironment()
				if e != nil {
					config.Environment = append(config.Environment, *e)
				}
			}
		}
	}

	config.Tag = GetNewImageName(instance, config)
	config.RevisionId = GetNewRevisionId(instance, config)
	config.SourceURL = GetNewSourceURL(instance, config)

	err := r.setDockerSecret(instance, &config)
	if err != nil {
		return nil, err
	}
	err = r.setGitSecret(instance, &config)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	instanceUidSlice := strings.Split(string(instance.UID), "-")
	configMapName := instance.Name + fmt.Sprintf("-%s", instanceUidSlice[len(instanceUidSlice)-1]) + "-configmap"
	dataMap := make(map[string]string)
	dataMap[ConfigDataKey] = string(data)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: instance.ObjectMeta.Namespace,
		},
		Data: dataMap,
	}
	return configMap, nil
}

type JobTemplateData struct {
	ObjectMetaName                     string
	ObjectMetaNamespace                string
	SpecTemplateObjectMetaLabelJobName string
	SpecTemplateSpecServiceAccountName string
	ContainerS2IRunImage               string
	SpecBackoffLimit                   int32
	ConfigMapName                      string
}

func (r *ReconcileS2iRun) getJobTemplateData(instance *devopsv1alpha1.S2iRun) (*JobTemplateData, error) {
	instanceUidSlice := strings.Split(string(instance.UID), "-")
	configMapName := instance.Name + fmt.Sprintf("-%s", instanceUidSlice[len(instanceUidSlice)-1]) + "-configmap"
	jobName := instance.Name + fmt.Sprintf("-%s", instanceUidSlice[len(instanceUidSlice)-1]) + "-job"
	imageName := os.Getenv("S2IIMAGENAME")
	if imageName == "" {
		return nil, fmt.Errorf("Failed to get s2i-image name, please set the env 'S2IIMAGENAME' ")
	}

	data := &JobTemplateData{
		ObjectMetaName:                     jobName,
		ObjectMetaNamespace:                instance.ObjectMeta.Namespace,
		SpecTemplateObjectMetaLabelJobName: jobName,
		SpecTemplateSpecServiceAccountName: RegularServiceAccount,
		ContainerS2IRunImage:               imageName,
		SpecBackoffLimit:                   instance.Spec.BackoffLimit,
		ConfigMapName:                      configMapName,
	}
	return data, nil
}

func (r *ReconcileS2iRun) GenerateNewJob(instance *devopsv1alpha1.S2iRun, templatePath string) (*batchv1.Job, error) {
	templateData, err := r.getJobTemplateData(instance)
	if err != nil {
		return nil, err
	}
	templateContent, err := os.ReadFile(templatePath)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("job").Parse(string(templateContent))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	err = tmpl.Execute(&buf, templateData)
	if err != nil {
		return nil, err
	}
	var job batchv1.Job
	decoder := yaml.NewYAMLOrJSONDecoder(&buf, 4096)
	err = decoder.Decode(&job)
	if err != nil {
		return nil, err
	}
	if instance.Spec.SecondsAfterFinished > 0 {
		job.Spec.TTLSecondsAfterFinished = &instance.Spec.SecondsAfterFinished
	}
	return &job, err
}

// setDockerSecret setS2iConfig docker secret
func (r *ReconcileS2iRun) setDockerSecret(instance *devopsv1alpha1.S2iRun, config *devopsv1alpha1.S2iConfig) error {
	if config.PushAuthentication != nil && config.PushAuthentication.SecretRef != nil {
		secret := &corev1.Secret{}
		err := r.Get(context.TODO(), types.NamespacedName{
			Namespace: instance.Namespace, Name: config.PushAuthentication.SecretRef.Name}, secret)
		if err != nil {
			return err
		}
		entry, err := getDockerEntryFromDockerSecret(secret)
		if err != nil {
			return err
		}
		config.PushAuthentication.ServerAddress = entry.ServerAddress
		config.PushAuthentication.Username = entry.Username
		config.PushAuthentication.Password = entry.Password
		config.PushAuthentication.Email = entry.Email
		config.PushAuthentication.SecretRef = nil
	}

	if config.PullAuthentication != nil && config.PullAuthentication.SecretRef != nil {
		secret := &corev1.Secret{}
		err := r.Get(context.TODO(), types.NamespacedName{
			Namespace: instance.Namespace, Name: config.PullAuthentication.SecretRef.Name}, secret)
		if err != nil {
			return err
		}
		entry, err := getDockerEntryFromDockerSecret(secret)
		if err != nil {
			return err
		}
		config.PushAuthentication.ServerAddress = entry.ServerAddress
		config.PullAuthentication.Username = entry.Username
		config.PullAuthentication.Password = entry.Password
		config.PullAuthentication.Email = entry.Email
		config.PullAuthentication.SecretRef = nil
	}

	if config.IncrementalAuthentication != nil && config.IncrementalAuthentication.SecretRef != nil {
		secret := &corev1.Secret{}
		err := r.Get(context.TODO(), types.NamespacedName{
			Namespace: instance.Namespace, Name: config.IncrementalAuthentication.SecretRef.Name}, secret)
		if err != nil {
			return err
		}
		entry, err := getDockerEntryFromDockerSecret(secret)
		if err != nil {
			return err
		}
		config.PushAuthentication.ServerAddress = entry.ServerAddress
		config.IncrementalAuthentication.Username = entry.Username
		config.IncrementalAuthentication.Password = entry.Password
		config.IncrementalAuthentication.Email = entry.Email
		config.IncrementalAuthentication.SecretRef = nil
	}

	if config.RuntimeAuthentication != nil && config.RuntimeAuthentication.SecretRef != nil {
		secret := &corev1.Secret{}
		err := r.Get(context.TODO(), types.NamespacedName{
			Namespace: instance.Namespace, Name: config.RuntimeAuthentication.SecretRef.Name}, secret)
		if err != nil {
			return err
		}
		entry, err := getDockerEntryFromDockerSecret(secret)
		if err != nil {
			return err
		}
		config.PushAuthentication.ServerAddress = entry.ServerAddress
		config.RuntimeAuthentication.Username = entry.Username
		config.RuntimeAuthentication.Password = entry.Password
		config.RuntimeAuthentication.Email = entry.Email
		config.RuntimeAuthentication.SecretRef = nil
	}
	return nil
}

func setJobLabelAnnotations(instance *devopsv1alpha1.S2iRun, config devopsv1alpha1.S2iConfig, template *devopsv1alpha1.UserDefineTemplate, job *batchv1.Job) {
	description := ""
	imageName := GetNewImageName(instance, config)
	if template != nil {
		description = fmt.Sprintf("image %s 's build job, use template %s, s2iName %s", imageName, template.Name, instance.Name)
	} else {
		description = fmt.Sprintf("image %s 's build job, s2iName %s", imageName, instance.Name)
	}
	if job.Labels == nil {
		job.Labels = map[string]string{
			devopsv1alpha1.S2iRunLabel: instance.Name,
		}
	} else {
		job.Annotations[devopsv1alpha1.S2iRunLabel] = instance.Name
	}
	if job.Annotations == nil {
		job.Annotations = map[string]string{
			devopsv1alpha1.DescriptionAnnotations: description,
		}
	} else {
		job.Annotations[devopsv1alpha1.DescriptionAnnotations] = description
	}
}
func setJobLabelandToleration(job *batchv1.Job, config devopsv1alpha1.S2iConfig) {
	var taintKey = TaintKey
	var nodeAffinityKey = NodeAffinityKey
	var nodeAffinityValues = []string{NodeAffinityValue}

	if config.TaintKey != "" {
		taintKey = config.TaintKey
	}

	if config.NodeAffinityKey != "" {
		nodeAffinityKey = config.NodeAffinityKey
	}

	if config.NodeAffinityValues != nil {
		nodeAffinityValues = config.NodeAffinityValues
	}

	tolerationNoSchedule := corev1.Toleration{
		Key:      taintKey,
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	}

	tolerationPreferNoSchedule := corev1.Toleration{
		Key:      taintKey,
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectPreferNoSchedule,
	}

	tolerations := []corev1.Toleration{
		tolerationNoSchedule,
		tolerationPreferNoSchedule,
	}

	nodeSelectorRequirements := corev1.NodeSelectorRequirement{
		Key:      nodeAffinityKey,
		Operator: corev1.NodeSelectorOpIn,
		Values:   nodeAffinityValues,
	}

	preferredSchedulingTerms := corev1.PreferredSchedulingTerm{
		Weight: 1,
		Preference: corev1.NodeSelectorTerm{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				nodeSelectorRequirements,
			},
		},
	}

	preferredDuringSchedulingIgnoredDuringExecutions := []corev1.PreferredSchedulingTerm{
		preferredSchedulingTerms,
	}

	affinity := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: preferredDuringSchedulingIgnoredDuringExecutions,
		},
	}

	job.Spec.Template.Spec.Affinity = affinity
	job.Spec.Template.Spec.Tolerations = tolerations

}
func setConfigMapLabelAnnotations(instance *devopsv1alpha1.S2iRun, config devopsv1alpha1.S2iConfig, template *devopsv1alpha1.UserDefineTemplate, cm *corev1.ConfigMap) {
	description := ""
	imageName := GetNewImageName(instance, config)
	if template != nil {
		description = fmt.Sprintf("image %s 's build configmap, use template %s, s2iName %s", imageName, template.Name, instance.Name)
	} else {
		description = fmt.Sprintf("image %s 's build configmap, s2iName %s", imageName, instance.Name)
	}
	if cm.Labels == nil {
		cm.Labels = map[string]string{
			devopsv1alpha1.S2iRunLabel: instance.Name,
		}
	} else {
		cm.Annotations[devopsv1alpha1.S2iRunLabel] = instance.Name
	}
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{
			devopsv1alpha1.DescriptionAnnotations: description,
		}
	} else {
		cm.Annotations[devopsv1alpha1.DescriptionAnnotations] = description
	}
}

// setGitSecret set GitClone Secret
func (r *ReconcileS2iRun) setGitSecret(instance *devopsv1alpha1.S2iRun, config *devopsv1alpha1.S2iConfig) error {
	if config.GitSecretRef != nil {
		secret := &corev1.Secret{}
		err := r.Get(context.TODO(), types.NamespacedName{
			Namespace: instance.Namespace, Name: config.GitSecretRef.Name}, secret)
		if err != nil {
			return err
		}

		username, ok := secret.Data[corev1.BasicAuthUsernameKey]
		if !ok {
			return fmt.Errorf("could not get username in secret %s", secret.Name)
		}
		password, ok := secret.Data[corev1.BasicAuthPasswordKey]
		if !ok {
			return fmt.Errorf("could not get password in secret %s", secret.Name)
		}
		sourceUrl, err := url.Parse(config.SourceURL)
		if err != nil {
			return err
		}
		config.SourceURL = fmt.Sprintf("%s://%s:%s@%s%s", sourceUrl.Scheme, url.QueryEscape(string(username)), url.QueryEscape(string(password)), sourceUrl.Host, sourceUrl.RequestURI())

	}
	return nil
}

func getDockerEntryFromDockerSecret(instance *corev1.Secret) (dockerConfigEntry *devopsv1alpha1.DockerConfigEntry, err error) {

	if instance.Type != corev1.SecretTypeDockerConfigJson {
		return nil, fmt.Errorf("secret %s in ns %s type should be %s",
			instance.Name, instance.Namespace, corev1.SecretTypeDockerConfigJson)
	}
	dockerConfigBytes, ok := instance.Data[corev1.DockerConfigJsonKey]
	if !ok {
		return nil, fmt.Errorf("could not get data %s", corev1.DockerConfigJsonKey)
	}
	dockerConfig := &devopsv1alpha1.DockerConfigJson{}
	err = json.Unmarshal(dockerConfigBytes, dockerConfig)
	if err != nil {
		return nil, err
	}
	if len(dockerConfig.Auths) == 0 {
		return nil, fmt.Errorf("docker config auth len should not be 0")
	}
	for registryAddress, dockerConfigEntry := range dockerConfig.Auths {
		dockerConfigEntry.ServerAddress = registryAddress
		return dockerConfigEntry.DeepCopy(), nil
	}
	return nil, nil
}
