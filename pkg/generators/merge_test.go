package generators

import (
	"encoding/json"
	"fmt"
	"testing"

	argoprojiov1alpha1 "github.com/argoproj-labs/applicationset/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

func getNestedListGenerator(json string) *argoprojiov1alpha1.ApplicationSetNestedGenerator {
	return &argoprojiov1alpha1.ApplicationSetNestedGenerator{
		List: &argoprojiov1alpha1.ListGenerator{
			Elements: []apiextensionsv1.JSON{{Raw: []byte(json)}},
		},
	}
}

func getTerminalListGeneratorMultiple(jsons []string) argoprojiov1alpha1.ApplicationSetTerminalGenerator {
	elements := make([]apiextensionsv1.JSON, len(jsons))

	for i, json := range jsons {
		elements[i] = apiextensionsv1.JSON{Raw: []byte(json)}
	}

	generator := argoprojiov1alpha1.ApplicationSetTerminalGenerator{
		List: &argoprojiov1alpha1.ListGenerator{
			Elements: elements,
		},
	}

	return generator
}

func listOfMapsToSet(maps []map[string]string) (map[string]bool, error) {
	set := make(map[string]bool, len(maps))
	for _, paramMap := range maps {
		paramMapAsJson, err := json.Marshal(paramMap)
		if err != nil {
			return nil, err
		}

		set[string(paramMapAsJson)] = false
	}
	return set, nil
}

func TestMergeGenerate(t *testing.T) {

	testCases := []struct {
		name           string
		baseGenerators []argoprojiov1alpha1.ApplicationSetNestedGenerator
		mergeKeys      []string
		expectedErr    error
		expected       []map[string]string
	}{
		{
			name:           "no generators",
			baseGenerators: []argoprojiov1alpha1.ApplicationSetNestedGenerator{},
			mergeKeys:      []string{"b"},
			expectedErr:    LessThanTwoGeneratorsInMerge,
		},
		{
			name: "one generator",
			baseGenerators: []argoprojiov1alpha1.ApplicationSetNestedGenerator{
				*getNestedListGenerator(`{"a": "1_1","b": "same","c": "1_3"}`),
			},
			mergeKeys:   []string{"b"},
			expectedErr: LessThanTwoGeneratorsInMerge,
		},
		{
			name: "happy flow - generate paramSets",
			baseGenerators: []argoprojiov1alpha1.ApplicationSetNestedGenerator{
				*getNestedListGenerator(`{"a": "1_1","b": "same","c": "1_3"}`),
				*getNestedListGenerator(`{"a": "2_1","b": "same"}`),
				*getNestedListGenerator(`{"a": "3_1","b": "different","c": "3_3"}`), // gets ignored because its merge key value isn't in the base params set
			},
			mergeKeys: []string{"b"},
			expected: []map[string]string{
				{"a": "2_1", "b": "same", "c": "1_3"},
			},
		},
		{
			name: "merge keys absent - do not merge",
			baseGenerators: []argoprojiov1alpha1.ApplicationSetNestedGenerator{
				*getNestedListGenerator(`{"a": "a"}`),
				*getNestedListGenerator(`{"a": "a"}`),
			},
			mergeKeys: []string{"b"},
			expected: []map[string]string{
				{"a": "a"},
			},
		},
		{
			name: "merge key present in first set, absent in second - do not merge",
			baseGenerators: []argoprojiov1alpha1.ApplicationSetNestedGenerator{
				*getNestedListGenerator(`{"a": "a"}`),
				*getNestedListGenerator(`{"b": "b"}`),
			},
			mergeKeys: []string{"b"},
			expected: []map[string]string{
				{"a": "a"},
			},
		},
		{
			name: "merge nested matrix with some lists",
			baseGenerators: []argoprojiov1alpha1.ApplicationSetNestedGenerator{
				{
					Matrix: &argoprojiov1alpha1.NestedMatrixGenerator{
						Generators: []argoprojiov1alpha1.ApplicationSetTerminalGenerator{
							getTerminalListGeneratorMultiple([]string{`{"a": "1"}`, `{"a": "2"}`}),
							getTerminalListGeneratorMultiple([]string{`{"b": "1"}`, `{"b": "2"}`}),
						},
					},
				},
				*getNestedListGenerator(`{"a": "1", "b": "1", "c": "added"}`),
			},
			mergeKeys: []string{"a", "b"},
			expected: []map[string]string{
				{"a": "1", "b": "1", "c": "added"},
				{"a": "1", "b": "2"},
				{"a": "2", "b": "1"},
				{"a": "2", "b": "2"},
			},
		},
		{
			name: "merge nested merge with some lists",
			baseGenerators: []argoprojiov1alpha1.ApplicationSetNestedGenerator{
				{
					Merge: &argoprojiov1alpha1.NestedMergeGenerator{
						MergeKeys: []string{"a"},
						Generators: []argoprojiov1alpha1.ApplicationSetTerminalGenerator{
							getTerminalListGeneratorMultiple([]string{`{"a": "1", "b": "1"}`, `{"a": "2", "b": "2"}`}),
							getTerminalListGeneratorMultiple([]string{`{"a": "1", "b": "3", "c": "added"}`, `{"a": "3", "b": "2"}`}), // First gets merged, second gets ignored
						},
					},
				},
				*getNestedListGenerator(`{"a": "1", "b": "3", "d": "added"}`),
			},
			mergeKeys: []string{"a", "b"},
			expected: []map[string]string{
				{"a": "1", "b": "3", "c": "added", "d": "added"},
				{"a": "2", "b": "2"},
			},
		},
	}

	for _, testCase := range testCases {
		testCaseCopy := testCase // since tests may run in parallel

		t.Run(testCaseCopy.name, func(t *testing.T) {
			t.Parallel()

			appSet := &argoprojiov1alpha1.ApplicationSet{}

			var mergeGenerator = NewMergeGenerator(
				map[string]Generator{
					"List": &ListGenerator{},
					"Matrix": &MatrixGenerator{
						supportedGenerators: map[string]Generator{
							"List": &ListGenerator{},
						},
					},
					"Merge": &MergeGenerator{
						supportedGenerators: map[string]Generator{
							"List": &ListGenerator{},
						},
					},
				},
			)

			got, err := mergeGenerator.GenerateParams(&argoprojiov1alpha1.ApplicationSetGenerator{
				Merge: &argoprojiov1alpha1.MergeGenerator{
					Generators: testCaseCopy.baseGenerators,
					MergeKeys:  testCaseCopy.mergeKeys,
					Template:   argoprojiov1alpha1.ApplicationSetTemplate{},
				},
			}, appSet)

			if testCaseCopy.expectedErr != nil {
				assert.EqualError(t, err, testCaseCopy.expectedErr.Error())
			} else {
				expectedSet, err := listOfMapsToSet(testCaseCopy.expected)
				assert.NoError(t, err)

				actualSet, err := listOfMapsToSet(got)
				assert.NoError(t, err)

				assert.NoError(t, err)
				assert.Equal(t, expectedSet, actualSet)
			}
		})
	}
}

func TestParamSetsAreUniqueByMergeKeys(t *testing.T) {
	testCases := []struct {
		name        string
		mergeKeys   []string
		paramSets   []map[string]string
		expectedErr error
		expected    map[string]map[string]string
	}{
		{
			name:        "no merge keys",
			mergeKeys:   []string{},
			expectedErr: NoMergeKeys,
		},
		{
			name:      "no paramSets",
			mergeKeys: []string{"key"},
			expected:  make(map[string]map[string]string),
		},
		{
			name:      "simple key, unique paramSets",
			mergeKeys: []string{"key"},
			paramSets: []map[string]string{{"key": "a"}, {"key": "b"}},
			expected: map[string]map[string]string{
				`{"key":"a"}`: {"key": "a"},
				`{"key":"b"}`: {"key": "b"},
			},
		},
		{
			name:        "simple key, non-unique paramSets",
			mergeKeys:   []string{"key"},
			paramSets:   []map[string]string{{"key": "a"}, {"key": "b"}, {"key": "b"}},
			expectedErr: fmt.Errorf("%w. Duplicate key was %s", NonUniqueParamSets, `{"key":"b"}`),
		},
		{
			name:      "simple key, duplicated key name, unique paramSets",
			mergeKeys: []string{"key", "key"},
			paramSets: []map[string]string{{"key": "a"}, {"key": "b"}},
			expected: map[string]map[string]string{
				`{"key":"a"}`: {"key": "a"},
				`{"key":"b"}`: {"key": "b"},
			},
		},
		{
			name:        "simple key, duplicated key name, non-unique paramSets",
			mergeKeys:   []string{"key", "key"},
			paramSets:   []map[string]string{{"key": "a"}, {"key": "b"}, {"key": "b"}},
			expectedErr: fmt.Errorf("%w. Duplicate key was %s", NonUniqueParamSets, `{"key":"b"}`),
		},
		{
			name:      "compound key, unique paramSets",
			mergeKeys: []string{"key1", "key2"},
			paramSets: []map[string]string{
				{"key1": "a", "key2": "a"},
				{"key1": "a", "key2": "b"},
				{"key1": "b", "key2": "a"},
			},
			expected: map[string]map[string]string{
				`{"key1":"a","key2":"a"}`: {"key1": "a", "key2": "a"},
				`{"key1":"a","key2":"b"}`: {"key1": "a", "key2": "b"},
				`{"key1":"b","key2":"a"}`: {"key1": "b", "key2": "a"},
			},
		},
		{
			name:      "compound key, duplicate key names, unique paramSets",
			mergeKeys: []string{"key1", "key1", "key2"},
			paramSets: []map[string]string{
				{"key1": "a", "key2": "a"},
				{"key1": "a", "key2": "b"},
				{"key1": "b", "key2": "a"},
			},
			expected: map[string]map[string]string{
				`{"key1":"a","key2":"a"}`: {"key1": "a", "key2": "a"},
				`{"key1":"a","key2":"b"}`: {"key1": "a", "key2": "b"},
				`{"key1":"b","key2":"a"}`: {"key1": "b", "key2": "a"},
			},
		},
		{
			name:      "compound key, non-unique paramSets",
			mergeKeys: []string{"key1", "key2"},
			paramSets: []map[string]string{
				{"key1": "a", "key2": "a"},
				{"key1": "a", "key2": "a"},
				{"key1": "b", "key2": "a"},
			},
			expectedErr: fmt.Errorf("%w. Duplicate key was %s", NonUniqueParamSets, `{"key1":"a","key2":"a"}`),
		},
		{
			name:      "compound key, duplicate key names, non-unique paramSets",
			mergeKeys: []string{"key1", "key1", "key2"},
			paramSets: []map[string]string{
				{"key1": "a", "key2": "a"},
				{"key1": "a", "key2": "a"},
				{"key1": "b", "key2": "a"},
			},
			expectedErr: fmt.Errorf("%w. Duplicate key was %s", NonUniqueParamSets, `{"key1":"a","key2":"a"}`),
		},
	}

	for _, testCase := range testCases {
		testCaseCopy := testCase // since tests may run in parallel

		t.Run(testCaseCopy.name, func(t *testing.T) {
			t.Parallel()

			got, err := getParamSetsByMergeKey(testCaseCopy.mergeKeys, testCaseCopy.paramSets)

			if testCaseCopy.expectedErr != nil {
				assert.EqualError(t, err, testCaseCopy.expectedErr.Error())
			} else {
				assert.NoError(t, err)
				assert.Equal(t, testCaseCopy.expected, got)
			}

		})

	}
}
